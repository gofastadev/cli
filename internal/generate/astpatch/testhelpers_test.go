package astpatch

type stubAstErr string

func (s stubAstErr) Error() string { return string(s) }
