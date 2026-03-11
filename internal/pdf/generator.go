package pdf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IrwantoCia/pdf-cv/internal/cv"
)

type Generator struct {
	renderer    *Renderer
	pdfLaTeXBin string
	timeout     time.Duration
}

func NewGenerator(renderer *Renderer, timeout time.Duration) (*Generator, error) {
	if renderer == nil {
		return nil, fmt.Errorf("renderer is required")
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	pdflatexPath, err := exec.LookPath("pdflatex")
	if err != nil {
		return nil, fmt.Errorf("pdflatex is not installed: %w", err)
	}

	return &Generator{
		renderer:    renderer,
		pdfLaTeXBin: pdflatexPath,
		timeout:     timeout,
	}, nil
}

func (g *Generator) Generate(ctx context.Context, workDir, jobName string, resume cv.Resume) ([]byte, error) {
	if strings.TrimSpace(workDir) == "" {
		return nil, fmt.Errorf("work directory is required")
	}
	if strings.TrimSpace(jobName) == "" {
		return nil, fmt.Errorf("job name is required")
	}

	texPath := filepath.Join(workDir, jobName+".tex")
	if err := g.renderer.Render(ctx, texPath, resume); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, fmt.Errorf("create pdf build directory: %w", err)
	}

	compileCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(
		compileCtx,
		g.pdfLaTeXBin,
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-no-shell-escape",
		"-output-directory", workDir,
		"-jobname", jobName,
		texPath,
	)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		log := strings.TrimSpace(string(out))
		if len(log) > 1200 {
			log = log[len(log)-1200:]
		}
		return nil, fmt.Errorf("pdflatex compile failed: %w: %s", err, log)
	}

	pdfPath := filepath.Join(workDir, jobName+".pdf")
	stat, err := os.Stat(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read generated pdf: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("generated pdf path is a directory")
	}

	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read generated pdf data: %w", err)
	}

	return pdfData, nil
}
