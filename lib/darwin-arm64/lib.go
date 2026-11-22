//go:build darwin && arm64

package darwin_arm64

// #cgo LDFLAGS: ${SRCDIR}/libudlex_capi.a -liconv
import "C"
