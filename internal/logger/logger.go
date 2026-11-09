package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Chave privada para garantir que ninguém mais mexa nesse valor do contexto
type ctxKey struct{}

var loggerKey = ctxKey{}

// FromContext recupera o logger do contexto.
// Se não houver nenhum, retorna o logger padrão (fallback seguro).
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// ToContext injeta um logger específico dentro do contexto.
func ToContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func With(ctx context.Context, args ...any) context.Context {
	log := FromContext(ctx)
	return ToContext(ctx, log.With(args...))
}

// InitGlobal inicializa o logger do sistema (pode chamar isso no main.go)
func InitGlobal(level, format string) error {
	var l slog.Level
	if level != "" {
		if err := l.UnmarshalText([]byte(level)); err != nil {
			return fmt.Errorf("logger: unknown log level %q: %w", level, err)
		}
	}

	opts := &slog.HandlerOptions{Level: l}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "", "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		return fmt.Errorf("logger: unknown log format %q", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}
