package udlex_test

import (
	"fmt"

	"github.com/gulldan/udlex-go"
)

func Example() {
	lexicon, err := udlex.Load("testdata/smoke-bundle", "eng")
	if err != nil {
		panic(err)
	}
	defer lexicon.Close()

	lemmas, err := lexicon.Lemmatize([]string{"cats", "mice", "unknown"})
	if err != nil {
		panic(err)
	}
	fmt.Println(lemmas)
	// Output: [cat mouse unknown]
}

func ExampleLanguage_Gain() {
	accuracy, baseline := 0.97, 0.62
	language := udlex.Language{Accuracy: &accuracy, Baseline: &baseline}

	if gain, measured := language.Gain(); measured {
		fmt.Printf("%.2f\n", gain)
	}
	// Output: 0.35
}

// Ядро читает диапазон форматов; что делать при отказе, говорит FormatRemedy.
func ExampleFormatSupported() {
	fmt.Println(udlex.FormatSupported(udlex.Constants().FormatVersion))

	remedy, err := udlex.FormatRemedy(udlex.Constants().FormatVersion + 100)
	if err != nil {
		return
	}
	fmt.Println(remedy != "")
	// Output:
	// true
	// true
}

func ExampleSource_IsKnown() {
	fmt.Println(udlex.SourceExact, udlex.SourceExact.IsKnown())
	fmt.Println(udlex.SourceRule, udlex.SourceRule.IsKnown())
	fmt.Println(udlex.SourceIdentity, udlex.SourceIdentity.IsKnown())
	// Output:
	// exact true
	// rule false
	// identity false
}

func ExampleSource_IsLemmatized() {
	fmt.Println(udlex.SourceExact, udlex.SourceExact.IsLemmatized())
	fmt.Println(udlex.SourceRule, udlex.SourceRule.IsLemmatized())
	fmt.Println(udlex.SourceIdentity, udlex.SourceIdentity.IsLemmatized())
	fmt.Println(udlex.SourceProtected, udlex.SourceProtected.IsLemmatized())
	// Output:
	// exact true
	// rule true
	// identity false
	// protected false
}
