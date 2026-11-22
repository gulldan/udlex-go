//go:build linux && amd64 && musl

package linux_musl_amd64

// #cgo LDFLAGS: ${SRCDIR}/libudlex_capi.a -lm
import "C"
