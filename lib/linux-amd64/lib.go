//go:build linux && amd64 && !musl

package linux_amd64

// #cgo LDFLAGS: ${SRCDIR}/libudlex_capi.a -lm -ldl -lpthread
import "C"
