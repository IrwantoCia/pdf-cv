package pdfqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IrwantoCia/pdf-cv/internal/cv"
	"github.com/IrwantoCia/pdf-cv/internal/pdf"
)

const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusExpired = "expired"
)

var (
	ErrQueueFull  = errors.New("queue is full")
	ErrNotFound   = errors.New("job not found")
	ErrNotReady   = errors.New("job is not ready")
	ErrJobExpired = errors.New("job has expired")
)

type Config struct {
	DB           *sql.DB
	Generator    *pdf.Generator
	WorkRoot     string
	QueueLimit   int
	ReadyTTL     time.Duration
	FailedTTL    time.Duration
	ExpiredTTL   time.Duration
	PollInterval time.Duration
}

type EnqueueMeta struct {
	ClientIP  string
	UserAgent string
}

type Job struct {
	ID         string
	Status     string
	Position   int
	Error      string
	ClientIP   string
	UserAgent  string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	ExpiresAt  *time.Time
}

type Service struct {
	db           *sql.DB
	generator    *pdf.Generator
	workRoot     string
	queueLimit   int
	readyTTL     time.Duration
	failedTTL    time.Duration
	expiredTTL   time.Duration
	pollInterval time.Duration
	wakeCh       chan struct{}
}

func NewService(cfg Config) (*Service, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("database is required")
	}
	if cfg.Generator == nil {
		return nil, fmt.Errorf("generator is required")
	}
	if strings.TrimSpace(cfg.WorkRoot) == "" {
		return nil, fmt.Errorf("work root is required")
	}
	if cfg.QueueLimit <= 0 {
		cfg.QueueLimit = 20
	}
	if cfg.ReadyTTL <= 0 {
		cfg.ReadyTTL = 2 * time.Minute
	}
	if cfg.FailedTTL <= 0 {
		cfg.FailedTTL = 2 * time.Minute
	}
	if cfg.ExpiredTTL <= 0 {
		cfg.ExpiredTTL = 30 * 24 * time.Hour
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}

	if err := os.MkdirAll(cfg.WorkRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create work root: %w", err)
	}

	s := &Service{
		db:           cfg.DB,
		generator:    cfg.Generator,
		workRoot:     cfg.WorkRoot,
		queueLimit:   cfg.QueueLimit,
		readyTTL:     cfg.ReadyTTL,
		failedTTL:    cfg.FailedTTL,
		expiredTTL:   cfg.ExpiredTTL,
		pollInterval: cfg.PollInterval,
		wakeCh:       make(chan struct{}, 1),
	}

	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.recoverOnStartup(ctx); err != nil {
		return err
	}

	go s.run(ctx)
	s.notifyWorker()

	return nil
}

func (s *Service) Enqueue(ctx context.Context, resume cv.Resume, meta EnqueueMeta) (Job, error) {
	payload, err := json.Marshal(resume)
	if err != nil {
		return Job{}, fmt.Errorf("marshal resume payload: %w", err)
	}

	meta.ClientIP = strings.TrimSpace(meta.ClientIP)
	if len(meta.ClientIP) > 64 {
		meta.ClientIP = meta.ClientIP[:64]
	}
	meta.UserAgent = strings.TrimSpace(meta.UserAgent)
	if len(meta.UserAgent) > 512 {
		meta.UserAgent = meta.UserAgent[:512]
	}

	now := time.Now().UTC()
	jobID, err := newJobID()
	if err != nil {
		return Job{}, fmt.Errorf("create job id: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin queue transaction: %w", err)
	}
	defer tx.Rollback()

	var activeCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM pdf_jobs WHERE status IN (?, ?)`, StatusQueued, StatusRunning).Scan(&activeCount); err != nil {
		return Job{}, fmt.Errorf("count active jobs: %w", err)
	}
	if activeCount >= s.queueLimit {
		return Job{}, ErrQueueFull
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO pdf_jobs (id, status, payload_json, error_message, client_ip, user_agent, created_at, started_at, finished_at, expires_at, updated_at, attempt_count)
		 VALUES (?, ?, ?, '', ?, ?, ?, NULL, NULL, NULL, ?, 0)`,
		jobID,
		StatusQueued,
		payload,
		meta.ClientIP,
		meta.UserAgent,
		now.Unix(),
		now.Unix(),
	); err != nil {
		return Job{}, fmt.Errorf("insert queued job: %w", err)
	}

	var position int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM pdf_jobs
		 WHERE status = ?
		   AND (created_at < ? OR (created_at = ? AND id <= ?))`,
		StatusQueued,
		now.Unix(),
		now.Unix(),
		jobID,
	).Scan(&position); err != nil {
		return Job{}, fmt.Errorf("calculate queue position: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit queued job: %w", err)
	}

	s.notifyWorker()

	return Job{
		ID:        jobID,
		Status:    StatusQueued,
		Position:  position,
		ClientIP:  meta.ClientIP,
		UserAgent: meta.UserAgent,
		CreatedAt: now,
	}, nil
}

func (s *Service) GetJob(ctx context.Context, id string) (Job, error) {
	if strings.TrimSpace(id) == "" {
		return Job{}, ErrNotFound
	}

	now := time.Now().UTC().Unix()

	var (
		job        Job
		createdAt  int64
		startedAt  sql.NullInt64
		finishedAt sql.NullInt64
		expiresAt  sql.NullInt64
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, status, error_message, client_ip, user_agent, created_at, started_at, finished_at, expires_at
		 FROM pdf_jobs
		 WHERE id = ?`,
		id,
	).Scan(&job.ID, &job.Status, &job.Error, &job.ClientIP, &job.UserAgent, &createdAt, &startedAt, &finishedAt, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("get job: %w", err)
	}

	job.CreatedAt = time.Unix(createdAt, 0).UTC()
	if startedAt.Valid {
		t := time.Unix(startedAt.Int64, 0).UTC()
		job.StartedAt = &t
	}
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0).UTC()
		job.FinishedAt = &t
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0).UTC()
		job.ExpiresAt = &t
		if expiresAt.Int64 <= now && (job.Status == StatusReady || job.Status == StatusFailed) {
			if markErr := s.markExpired(context.Background(), id); markErr == nil {
				job.Status = StatusExpired
			}
		}
	}

	if job.Status == StatusQueued {
		if err := s.db.QueryRowContext(
			ctx,
			`SELECT COUNT(1)
			 FROM pdf_jobs
			 WHERE status = ?
			   AND (created_at < ? OR (created_at = ? AND id <= ?))`,
			StatusQueued,
			job.CreatedAt.Unix(),
			job.CreatedAt.Unix(),
			job.ID,
		).Scan(&job.Position); err != nil {
			return Job{}, fmt.Errorf("calculate queue position: %w", err)
		}
	}

	return job, nil
}

func (s *Service) Download(ctx context.Context, id string) ([]byte, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrNotFound
	}

	now := time.Now().UTC().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin download transaction: %w", err)
	}
	defer tx.Rollback()

	var status string
	var expiresAt sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, expires_at FROM pdf_jobs WHERE id = ?`, id).Scan(&status, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lookup job for download: %w", err)
	}

	if status == StatusExpired {
		return nil, ErrJobExpired
	}
	if status != StatusReady {
		return nil, ErrNotReady
	}
	if expiresAt.Valid && expiresAt.Int64 <= now {
		if _, err := tx.ExecContext(ctx, `UPDATE pdf_jobs SET status = ?, updated_at = ? WHERE id = ?`, StatusExpired, now, id); err != nil {
			return nil, fmt.Errorf("expire outdated job: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pdf_job_results WHERE job_id = ?`, id); err != nil {
			return nil, fmt.Errorf("delete outdated result: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit outdated result deletion: %w", err)
		}
		return nil, ErrJobExpired
	}

	var pdfData []byte
	if err := tx.QueryRowContext(ctx, `SELECT pdf_blob FROM pdf_job_results WHERE job_id = ?`, id).Scan(&pdfData); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrJobExpired
		}
		return nil, fmt.Errorf("load pdf bytes: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM pdf_job_results WHERE job_id = ?`, id); err != nil {
		return nil, fmt.Errorf("cleanup downloaded result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pdf_jobs SET status = ?, updated_at = ?, expires_at = ? WHERE id = ?`, StatusExpired, now, now, id); err != nil {
		return nil, fmt.Errorf("mark downloaded job expired: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit download transaction: %w", err)
	}

	return pdfData, nil
}

func (s *Service) run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	cleanupTicker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wakeCh:
		case <-ticker.C:
		case <-cleanupTicker.C:
			_ = s.cleanupExpired(context.Background())
		}

		for {
			processed, err := s.processOne(ctx)
			if err != nil || !processed {
				break
			}
		}
	}
}

func (s *Service) processOne(ctx context.Context) (bool, error) {
	jobID, resume, err := s.claimNextJob(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	workDir, err := os.MkdirTemp(s.workRoot, jobID+"-")
	if err != nil {
		_ = s.markFailed(context.Background(), jobID, fmt.Errorf("create temporary work dir: %w", err))
		return true, nil
	}
	defer os.RemoveAll(workDir)

	pdfBytes, err := s.generator.Generate(ctx, filepath.Clean(workDir), "cv", resume)
	if err != nil {
		_ = s.markFailed(context.Background(), jobID, err)
		return true, nil
	}

	if err := s.markReady(context.Background(), jobID, pdfBytes); err != nil {
		_ = s.markFailed(context.Background(), jobID, err)
	}

	return true, nil
}

func (s *Service) claimNextJob(ctx context.Context) (string, cv.Resume, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", cv.Resume{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback()

	var (
		jobID   string
		payload []byte
	)
	if err := tx.QueryRowContext(
		ctx,
		`SELECT id, payload_json
		 FROM pdf_jobs
		 WHERE status = ?
		 ORDER BY created_at ASC, id ASC
		 LIMIT 1`,
		StatusQueued,
	).Scan(&jobID, &payload); err != nil {
		return "", cv.Resume{}, err
	}

	now := time.Now().UTC().Unix()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE pdf_jobs
		 SET status = ?, started_at = ?, updated_at = ?, attempt_count = attempt_count + 1
		 WHERE id = ? AND status = ?`,
		StatusRunning,
		now,
		now,
		jobID,
		StatusQueued,
	)
	if err != nil {
		return "", cv.Resume{}, fmt.Errorf("mark job running: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", cv.Resume{}, fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return "", cv.Resume{}, sql.ErrNoRows
	}

	if err := tx.Commit(); err != nil {
		return "", cv.Resume{}, fmt.Errorf("commit claimed job: %w", err)
	}

	var resume cv.Resume
	if err := json.Unmarshal(payload, &resume); err != nil {
		_ = s.markFailed(context.Background(), jobID, fmt.Errorf("decode job payload: %w", err))
		return "", cv.Resume{}, sql.ErrNoRows
	}

	return jobID, resume, nil
}

func (s *Service) markReady(ctx context.Context, jobID string, pdfData []byte) error {
	now := time.Now().UTC()
	expiresAt := now.Add(s.readyTTL).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ready transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO pdf_job_results (job_id, pdf_blob, size_bytes, created_at, expires_at, downloaded_at)
		 VALUES (?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(job_id) DO UPDATE
		 SET pdf_blob = excluded.pdf_blob,
		     size_bytes = excluded.size_bytes,
		     created_at = excluded.created_at,
		     expires_at = excluded.expires_at,
		     downloaded_at = NULL`,
		jobID,
		pdfData,
		len(pdfData),
		now.Unix(),
		expiresAt,
	); err != nil {
		return fmt.Errorf("store ready pdf: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE pdf_jobs
		 SET status = ?, error_message = '', finished_at = ?, expires_at = ?, updated_at = ?
		 WHERE id = ?`,
		StatusReady,
		now.Unix(),
		expiresAt,
		now.Unix(),
		jobID,
	); err != nil {
		return fmt.Errorf("update ready job state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ready transaction: %w", err)
	}

	return nil
}

func (s *Service) markFailed(ctx context.Context, jobID string, jobErr error) error {
	now := time.Now().UTC()
	expiresAt := now.Add(s.failedTTL).Unix()
	message := strings.TrimSpace(jobErr.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE pdf_jobs
		 SET status = ?, error_message = ?, finished_at = ?, expires_at = ?, updated_at = ?
		 WHERE id = ?`,
		StatusFailed,
		message,
		now.Unix(),
		expiresAt,
		now.Unix(),
		jobID,
	); err != nil {
		return fmt.Errorf("mark failed job: %w", err)
	}

	_, _ = s.db.ExecContext(ctx, `DELETE FROM pdf_job_results WHERE job_id = ?`, jobID)

	return nil
}

func (s *Service) markExpired(ctx context.Context, jobID string) error {
	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin expire transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pdf_job_results WHERE job_id = ?`, jobID); err != nil {
		return fmt.Errorf("delete expired result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pdf_jobs SET status = ?, updated_at = ?, expires_at = ? WHERE id = ?`, StatusExpired, now, now, jobID); err != nil {
		return fmt.Errorf("update expired status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit expire transaction: %w", err)
	}

	return nil
}

func (s *Service) recoverOnStartup(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE pdf_jobs
		 SET status = ?, started_at = NULL, updated_at = ?
		 WHERE status = ?`,
		StatusQueued,
		now,
		StatusRunning,
	); err != nil {
		return fmt.Errorf("recover running jobs: %w", err)
	}

	if err := s.cleanupExpired(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service) cleanupExpired(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pdf_job_results WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("cleanup expired job results: %w", err)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE pdf_jobs
		 SET status = ?, updated_at = ?
		 WHERE status IN (?, ?) AND expires_at IS NOT NULL AND expires_at <= ?`,
		StatusExpired,
		now,
		StatusReady,
		StatusFailed,
		now,
	); err != nil {
		return fmt.Errorf("cleanup expired jobs: %w", err)
	}

	cutoff := now - int64(s.expiredTTL/time.Second)
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM pdf_jobs
		 WHERE status = ?
		   AND updated_at <= ?`,
		StatusExpired,
		cutoff,
	); err != nil {
		return fmt.Errorf("cleanup old expired jobs: %w", err)
	}

	return nil
}

func (s *Service) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("enable sqlite wal mode: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS pdf_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			payload_json BLOB NOT NULL,
			error_message TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			finished_at INTEGER,
			expires_at INTEGER,
			updated_at INTEGER NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0
		);
	`); err != nil {
		return fmt.Errorf("create pdf_jobs table: %w", err)
	}

	if err := s.ensurePDFJobsColumns(ctx); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS pdf_job_results (
			job_id TEXT PRIMARY KEY,
			pdf_blob BLOB NOT NULL,
			size_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			downloaded_at INTEGER,
			FOREIGN KEY(job_id) REFERENCES pdf_jobs(id) ON DELETE CASCADE
		);
	`); err != nil {
		return fmt.Errorf("create pdf_job_results table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_pdf_jobs_status_created ON pdf_jobs(status, created_at, id)`); err != nil {
		return fmt.Errorf("create status index: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_pdf_jobs_expires ON pdf_jobs(expires_at)`); err != nil {
		return fmt.Errorf("create jobs expire index: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_pdf_job_results_expires ON pdf_job_results(expires_at)`); err != nil {
		return fmt.Errorf("create results expire index: %w", err)
	}

	return nil
}

func (s *Service) ensurePDFJobsColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(pdf_jobs)`)
	if err != nil {
		return fmt.Errorf("inspect pdf_jobs schema: %w", err)
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			defaultV  sql.NullString
			primaryKV int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKV); err != nil {
			return fmt.Errorf("scan pdf_jobs schema: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pdf_jobs schema: %w", err)
	}

	if !existing["client_ip"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE pdf_jobs ADD COLUMN client_ip TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add pdf_jobs.client_ip column: %w", err)
		}
	}
	if !existing["user_agent"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE pdf_jobs ADD COLUMN user_agent TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add pdf_jobs.user_agent column: %w", err)
		}
	}

	return nil
}

func (s *Service) notifyWorker() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func newJobID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return "job_" + hex.EncodeToString(buf), nil
}
