package stackresolve

type ioErr string

func (e ioErr) Error() string { return string(e) }
