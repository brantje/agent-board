package redaction

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
)

type handlerSegment struct {
	group string
	attrs []slog.Attr
}

// SlogHandler applies the active execution redaction registry at the final
// application logging boundary. Ordered segments preserve the interleaving of
// WithAttrs and WithGroup calls while delaying sanitization until emission, so
// secrets registered after a logger is derived are still removed.
type SlogHandler struct {
	next     slog.Handler
	registry *Registry
	segments []handlerSegment
}

func NewSlogHandler(next slog.Handler, registry *Registry) slog.Handler {
	return &SlogHandler{next: next, registry: registry}
}

func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *SlogHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, h.redactString(record.Message), record.PC)
	segments := cloneSegments(h.segments)
	var recordAttrs []slog.Attr
	record.Attrs(func(attr slog.Attr) bool {
		recordAttrs = append(recordAttrs, attr)
		return true
	})
	if len(recordAttrs) > 0 {
		segments = append(segments, handlerSegment{attrs: recordAttrs})
	}
	redacted.AddAttrs(h.materialize(segments)...)
	return h.next.Handle(ctx, redacted)
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.segments = cloneSegments(h.segments)
	clone.segments = append(clone.segments, handlerSegment{attrs: append([]slog.Attr(nil), attrs...)})
	return &clone
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.segments = cloneSegments(h.segments)
	clone.segments = append(clone.segments, handlerSegment{group: name})
	return &clone
}

func (h *SlogHandler) materialize(segments []handlerSegment) []slog.Attr {
	attrs := make([]slog.Attr, 0)
	for index, segment := range segments {
		if segment.group != "" {
			nested := h.materialize(segments[index+1:])
			if len(nested) > 0 {
				attrs = append(attrs, slog.Attr{Key: h.redactString(segment.group), Value: slog.GroupValue(nested...)})
			}
			return attrs
		}
		for _, attr := range segment.attrs {
			attrs = append(attrs, h.redactAttr(attr))
		}
	}
	return attrs
}

func (h *SlogHandler) redactAttr(attr slog.Attr) slog.Attr {
	attr.Key = h.redactString(attr.Key)
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(h.redactString(value.String()))
	case slog.KindGroup:
		group := value.Group()
		for index := range group {
			group[index] = h.redactAttr(group[index])
		}
		attr.Value = slog.GroupValue(group...)
	case slog.KindAny:
		typed := value.Any()
		if text, ok := byteSliceText(typed); ok {
			attr.Value = slog.StringValue(h.redactString(text))
		} else {
			attr.Value = slog.StringValue(h.redactString(fmt.Sprint(typed)))
		}
	default:
		attr.Value = value
	}
	return attr
}

func byteSliceText(value any) (string, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Slice || reflected.Type().Elem().Kind() != reflect.Uint8 {
		return "", false
	}
	bytes := make([]byte, reflected.Len())
	for index := range bytes {
		bytes[index] = byte(reflected.Index(index).Uint())
	}
	return string(bytes), true
}

func (h *SlogHandler) redactString(value string) string {
	if h.registry == nil {
		return value
	}
	return h.registry.RedactAllString(value)
}

func cloneSegments(segments []handlerSegment) []handlerSegment {
	cloned := make([]handlerSegment, len(segments))
	for index, segment := range segments {
		cloned[index].group = segment.group
		cloned[index].attrs = append([]slog.Attr(nil), segment.attrs...)
	}
	return cloned
}
