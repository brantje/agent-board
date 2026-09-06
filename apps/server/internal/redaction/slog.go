package redaction

import (
	"context"
	"fmt"
	"log/slog"
)

// SlogHandler applies the active execution redaction registry at the final
// application logging boundary. It keeps contextual attributes/groups locally
// so values registered after a logger is derived are still sanitized when a
// record is emitted.
type SlogHandler struct {
	next     slog.Handler
	registry *Registry
	attrs    []slog.Attr
	groups   []string
}

func NewSlogHandler(next slog.Handler, registry *Registry) slog.Handler {
	return &SlogHandler{next: next, registry: registry}
}

func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *SlogHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, h.registry.RedactAllString(record.Message), record.PC)
	attrs := append([]slog.Attr(nil), h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	for index := range attrs {
		attrs[index] = h.redactAttr(attrs[index])
	}
	for index := len(h.groups) - 1; index >= 0; index-- {
		attrs = []slog.Attr{{
			Key:   h.registry.RedactAllString(h.groups[index]),
			Value: slog.GroupValue(attrs...),
		}}
	}
	redacted.AddAttrs(attrs...)
	return h.next.Handle(ctx, redacted)
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	clone.groups = append([]string(nil), h.groups...)
	return &clone
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.attrs = append([]slog.Attr(nil), h.attrs...)
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

func (h *SlogHandler) redactAttr(attr slog.Attr) slog.Attr {
	attr.Key = h.registry.RedactAllString(attr.Key)
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(h.registry.RedactAllString(value.String()))
	case slog.KindGroup:
		group := value.Group()
		for index := range group {
			group[index] = h.redactAttr(group[index])
		}
		attr.Value = slog.GroupValue(group...)
	case slog.KindAny:
		attr.Value = slog.StringValue(h.registry.RedactAllString(fmt.Sprint(value.Any())))
	default:
		attr.Value = value
	}
	return attr
}
