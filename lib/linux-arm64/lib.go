//go:build linux && arm64

package linux_arm64

// #cgo LDFLAGS: ${SRCDIR}/libudlex_capi.a -lm -ldl -lpthread
import "C"
