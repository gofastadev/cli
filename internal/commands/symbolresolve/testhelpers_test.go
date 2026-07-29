package symbolresolve

type stubErr string

func (e stubErr) Error() string { return string(e) }
