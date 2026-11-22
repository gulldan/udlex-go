//go:build darwin && amd64

package darwin_amd64

// #cgo LDFLAGS: ${SRCDIR}/libudlex_capi.a -liconv
import "C"
