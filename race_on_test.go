//go:build race

package udlex_test

// Под детектором гонок счётчик аллокаций меряет сам детектор.
const raceDetector = true
