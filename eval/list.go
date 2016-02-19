package eval

import (
	"bytes"
	"errors"
	"strconv"
)

// Error definitions.
var (
	ErrNeedIntIndex    = errors.New("need integer index")
	ErrIndexOutOfRange = errors.New("index out of range")
)

// List is a list of Value's.
type List struct {
	inner *[]Value
}

// NewList creates a new List.
func NewList(vs ...Value) List {
	return List{&vs}
}

func (List) Kind() string {
	return "list"
}

func (l List) appendStrings(ss []string) {
	for _, s := range ss {
		*l.inner = append(*l.inner, String(s))
	}
}

func (l List) Repr() string {
	var b ListReprBuilder
	for _, v := range *l.inner {
		b.WriteElem(v.Repr())
	}
	return b.String()
}

func (l List) IndexOne(idx Value) Value {
	i := intIndexWithin(idx, len(*l.inner))
	return (*l.inner)[i]
}

func (l List) IndexSet(idx Value, v Value) {
	i := intIndexWithin(idx, len(*l.inner))
	(*l.inner)[i] = v
}

func intIndexWithin(idx Value, n int) int {
	i := intIndex(idx)

	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		throw(ErrIndexOutOfRange)
	}
	return i
}

func intIndex(idx Value) int {
	i, err := strconv.Atoi(ToString(idx))
	if err != nil {
		err := err.(*strconv.NumError)
		if err.Err == strconv.ErrRange {
			throw(ErrIndexOutOfRange)
		} else {
			throw(ErrNeedIntIndex)
		}
	}
	return i
}

// ListReprBuilder helps to build Repr of list-like Values.
type ListReprBuilder struct {
	buf bytes.Buffer
}

func (b *ListReprBuilder) WriteElem(v string) {
	if b.buf.Len() == 0 {
		b.buf.WriteByte('[')
	} else {
		b.buf.WriteByte(' ')
	}
	b.buf.WriteString(v)
}

func (b *ListReprBuilder) String() string {
	if b.buf.Len() == 0 {
		return "[]"
	}
	b.buf.WriteByte(']')
	return b.buf.String()
}