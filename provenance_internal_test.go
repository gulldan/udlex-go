package udlex

import (
	"errors"
	"testing"
)

// Каждый код C ABI декодируется, даже если в тестовом бандле он не встречается;
// неизвестный — нарушение ABI, а не правдоподобный ответ.
func TestDecodeSource(t *testing.T) {
	for code := range uint8(SourceContext) + 1 {
		if source, err := decodeSource("test", code); err != nil || source != Source(code) {
			t.Fatalf("код %d: %v, %v", code, source, err)
		}
	}
	for _, code := range []uint8{uint8(SourceContext) + 1, 127, 255} {
		var failure *Error
		if _, err := decodeSource("test", code); !errors.As(err, &failure) || failure.Code != CodeABI {
			t.Fatalf("код %d: %v, ожидался CodeABI", code, err)
		}
	}
}
