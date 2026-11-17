package core_ext

import "testing"

func TestDesCrypt(t *testing.T) {
	for _, c := range [][3]string{
		{"", "aa", "aaQSqAReePlq6"}, {"nutmeg", "Mi", "MiqkFWCm1fNJI"}, {"ellen1", "ri", "ri79kNd7V6.Sk"},
		{"Sharon", "./", "./UY9Q7TvYJDg"}, {"norahs", "am", "amfIADT2iqjA."}, {"norahs", "7a", "7azfT5tIdyh0I"},
		{"0123456789", "aa", "aa4c4gpuvCkSE"}, {"hello world", "aab", "aayPz4hyPS1wI"}, {"secret", "ab", "abNANd1rDfiNc"},
	} {
		if got := desCrypt([]byte(c[0]), []byte(c[1])); got != c[2] {
			t.Errorf("crypt(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
