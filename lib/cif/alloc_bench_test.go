package cif

import "testing"

func BenchmarkParseExtract4MiB(b *testing.B) {
	text := syntheticMMCIF(4 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := Extract(ParseTruncated(text))
		if err != nil || len(result.Cofactors) != 1 {
			b.Fatal(err, result.Cofactors)
		}
	}
}

func BenchmarkScalarValue40MiB(b *testing.B) {
	text := syntheticMMCIF(40 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, ok := ScalarValue(text, "_refine.ls_R_factor_R_free"); !ok {
			b.Fatal("scalar missing")
		}
	}
}

func BenchmarkExtractText4MiB(b *testing.B) {
	text := syntheticMMCIF(4 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, result, err := ExtractText(text, true)
		if err != nil || len(result.Cofactors) != 1 {
			b.Fatal(err, result.Cofactors)
		}
	}
}
