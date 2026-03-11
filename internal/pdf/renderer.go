package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/IrwantoCia/pdf-cv/internal/cv"
)

type Renderer struct {
	tmpl *template.Template
}

func NewRenderer(templatePath string) (*Renderer, error) {
	tmpl, err := template.New(filepath.Base(templatePath)).Funcs(template.FuncMap{
		"latex": escapeLaTeX,
	}).Option("missingkey=error").ParseFiles(templatePath)
	if err != nil {
		return nil, fmt.Errorf("parse tex template: %w", err)
	}

	return &Renderer{tmpl: tmpl}, nil
}

func (r *Renderer) Render(ctx context.Context, outputPath string, resume cv.Resume) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create tex output directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "cv-*.tex")
	if err != nil {
		return fmt.Errorf("create temporary tex file: %w", err)
	}
	tmpName := tmp.Name()

	if err := r.tmpl.Execute(tmp, resume); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("render tex template: %w", err)
	}

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("set tex file permissions: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temporary tex file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, outputPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace tex file: %w", err)
	}

	return nil
}

func escapeLaTeX(input string) string {
	if input == "" {
		return ""
	}

	s := strings.ReplaceAll(input, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")

	replacer := strings.NewReplacer(
		"\\", `\textbackslash{}`,
		"{", `\{`,
		"}", `\}`,
		"#", `\#`,
		"$", `\$`,
		"%", `\%`,
		"&", `\&`,
		"_", `\_`,
		"~", `\textasciitilde{}`,
		"^", `\textasciicircum{}`,
	)

	return replacer.Replace(s)
}
