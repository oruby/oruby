package compiler

// ProgressFunc reports how far a compilation stage is: stage is a short name
// ("resolve", "analyze", "emit", "write"), done of total items are finished
// (total is 0 when it is not known yet) and item names the one just finished
// or started. A nil ProgressFunc reports nothing.
type ProgressFunc func(stage string, done, total int, item string)

func (f ProgressFunc) report(stage string, done, total int, item string) {
	if f != nil {
		f(stage, done, total, item)
	}
}
