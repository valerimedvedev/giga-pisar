package asr

import (
	"errors"
	"strings"
)

// Tokenizer — кусочки слов из *_tokenizer.model (protobuf SentencePiece):
// ModelProto { repeated SentencePiece pieces = 1 }, SentencePiece { string piece = 1 }.
type Tokenizer struct{ Pieces []string }

func varint(d []byte, i int) (uint64, int, bool) {
	var r uint64
	shift := 0
	for i < len(d) {
		b := d[i]
		i++
		r |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return r, i, true
		}
		shift += 7
		if shift > 63 {
			return 0, i, false
		}
	}
	return 0, i, false
}

func skipField(d []byte, i int, wire int) (int, bool) {
	switch wire {
	case 0:
		_, j, ok := varint(d, i)
		return j, ok
	case 1:
		return i + 8, i+8 <= len(d)
	case 2:
		l, j, ok := varint(d, i)
		if !ok || j+int(l) > len(d) {
			return 0, false
		}
		return j + int(l), true
	case 5:
		return i + 4, i+4 <= len(d)
	}
	return 0, false
}

func piece(d []byte) string {
	i := 0
	for i < len(d) {
		t, j, ok := varint(d, i)
		if !ok {
			break
		}
		i = j
		if t>>3 == 1 && t&7 == 2 {
			l, j, ok := varint(d, i)
			if !ok {
				break
			}
			end := j + int(l)
			if end > len(d) {
				end = len(d)
			}
			return string(d[j:end])
		}
		i, ok = skipField(d, i, int(t&7))
		if !ok {
			break
		}
	}
	return ""
}

func NewTokenizer(bytes []byte) (*Tokenizer, error) {
	var out []string
	i := 0
	for i < len(bytes) {
		t, j, ok := varint(bytes, i)
		if !ok {
			break
		}
		i = j
		if t>>3 == 1 && t&7 == 2 {
			l, j, ok := varint(bytes, i)
			if !ok || j+int(l) > len(bytes) {
				break
			}
			out = append(out, piece(bytes[j:j+int(l)]))
			i = j + int(l)
		} else {
			i, ok = skipField(bytes, i, int(t&7))
			if !ok {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("не разобрал токенизатор")
	}
	return &Tokenizer{Pieces: out}, nil
}

func (t *Tokenizer) BlankID() int { return len(t.Pieces) }

func (t *Tokenizer) Decode(ids []int) string {
	var sb strings.Builder
	for _, id := range ids {
		if id >= 0 && id < len(t.Pieces) {
			sb.WriteString(t.Pieces[id])
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(sb.String(), "▁", " "))
}
