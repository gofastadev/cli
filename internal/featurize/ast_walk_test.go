package featurize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The applyOn* family is a hand-written dst walker: every statement and
// expression kind needs its own case, and a missing one means selectors nested
// in that construct are silently left un-rewritten. That failure is invisible
// in a small test — the file still parses and still renders — so it only shows
// up as a compile error in a user's project after the refactor has run.
//
// walkerKitchenSink therefore contains at least one instance of every node
// kind the walker claims to handle, each with a `repoInterfaces.` selector
// inside it. The assertion is simply that NO occurrence survives: any node
// kind the walker fails to descend into leaves its selector behind.

const walkerKitchenSink = `package services

import (
	"context"
	"sort"

	repoInterfaces "example.com/myapp/app/repositories/interfaces"
)

// ValueSpec with both a type and values, plus a TypeSpec.
var defaultRepo repoInterfaces.UserRepositoryInterface = nil

type Handler struct {
	repo repoInterfaces.UserRepositoryInterface
}

type Runner interface {
	Run(r repoInterfaces.UserRepositoryInterface) error
}

func (h *Handler) Everything(ctx context.Context, in []repoInterfaces.UserRepositoryInterface) error {
	// DeclStmt wrapping a GenDecl.
	var local repoInterfaces.UserRepositoryInterface

	// AssignStmt, both sides.
	local = defaultRepo
	a, b := repoInterfaces.ErrUserNotDeletable, repoInterfaces.ErrUserNotDeletable
	_, _ = a, b

	// ExprStmt containing a CallExpr with args.
	sort.Slice(in, func(i, j int) bool {
		// FuncLit body + BinaryExpr + IndexExpr.
		return in[i] != nil && in[j] == repoInterfaces.UserRepositoryInterface(nil)
	})

	// IfStmt with Init, Cond, Body and Else.
	if x := repoInterfaces.ErrUserNotDeletable; x != nil {
		_ = repoInterfaces.ErrUserNotDeletable
	} else if repoInterfaces.ErrUserNotDeletable != nil {
		_ = repoInterfaces.ErrUserNotDeletable
	} else {
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// ForStmt with Init, Cond, Post.
	for i := 0; i < len(in); i++ {
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// RangeStmt with Key, Value, X.
	for k, v := range map[string]repoInterfaces.UserRepositoryInterface{} {
		_, _ = k, v
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// SwitchStmt with Init, Tag and case bodies.
	switch y := len(in); y {
	case 1:
		_ = repoInterfaces.ErrUserNotDeletable
	default:
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// TypeSwitchStmt with Init and Assign.
	switch z := any(local); z.(type) {
	case repoInterfaces.UserRepositoryInterface:
		_ = repoInterfaces.ErrUserNotDeletable
	default:
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// BlockStmt.
	{
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// DeferStmt and GoStmt.
	defer noop(repoInterfaces.ErrUserNotDeletable)
	go noop(repoInterfaces.ErrUserNotDeletable)

	// SendStmt and IncDecStmt.
	ch := make(chan repoInterfaces.UserRepositoryInterface, 1)
	ch <- defaultRepo
	counter := 0
	counter++

	// LabeledStmt.
Loop:
	for range in {
		_ = repoInterfaces.ErrUserNotDeletable
		break Loop
	}

	// SelectStmt with a CommClause.
	select {
	case got := <-ch:
		_ = got
		_ = repoInterfaces.ErrUserNotDeletable
	default:
		_ = repoInterfaces.ErrUserNotDeletable
	}

	// Expression kinds: Unary, Paren, Star, SliceExpr, TypeAssert,
	// CompositeLit with KeyValueExpr, ArrayType, MapType, ChanType.
	_ = &Handler{repo: defaultRepo}
	_ = (repoInterfaces.ErrUserNotDeletable)
	var ptr *repoInterfaces.UserRepositoryInterface
	_ = ptr
	_ = in[0:1]
	_ = in[0:1:1]
	_ = any(local).(repoInterfaces.UserRepositoryInterface)
	_ = []repoInterfaces.UserRepositoryInterface{}
	_ = [2]repoInterfaces.UserRepositoryInterface{}
	_ = map[string]repoInterfaces.UserRepositoryInterface{"k": defaultRepo}
	_ = make(chan repoInterfaces.UserRepositoryInterface)
	_ = struct{ R repoInterfaces.UserRepositoryInterface }{}
	_ = func(r repoInterfaces.UserRepositoryInterface) repoInterfaces.UserRepositoryInterface { return r }

	return nil
}

func noop(err error) {}
`

func TestApplyOnStmt_WalksEveryStatementKind(t *testing.T) {
	got, err := TransformPerResource([]byte(walkerKitchenSink), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	require.NoError(t, err)

	out := string(got)
	assert.NotContains(t, out, "repoInterfaces.",
		"a selector survived the walk — some statement or expression kind is not being descended into")
	assert.Contains(t, out, "package user", "package decl must be rewritten")
	assert.NotContains(t, out, `"example.com/myapp/app/repositories/interfaces"`,
		"the collapsed import must be dropped once every reference is gone")
}

// TestApplyOnSpec_WalksValueAndTypeSpecs isolates the declaration walker: a
// package-level var with an explicit type and an initializer, and a type
// declaration, all of which live in GenDecls rather than function bodies.
func TestApplyOnSpec_WalksValueAndTypeSpecs(t *testing.T) {
	src := `package services

import repoInterfaces "example.com/myapp/app/repositories/interfaces"

var typed repoInterfaces.UserRepositoryInterface
var initialized = repoInterfaces.ErrUserNotDeletable
var (
	grouped repoInterfaces.UserRepositoryInterface
	pair    = []repoInterfaces.UserRepositoryInterface{}
)

type Aliased repoInterfaces.UserRepositoryInterface

const marker = "repoInterfaces.NotASelector"
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	require.NoError(t, err)

	out := string(got)
	// The string constant must survive verbatim — identifier-aware rewriting is
	// the whole reason this package uses an AST rather than regex.
	assert.Contains(t, out, `"repoInterfaces.NotASelector"`,
		"a string literal that merely looks like a selector must not be rewritten")

	withoutStringLiteral := strings.ReplaceAll(out, `"repoInterfaces.NotASelector"`, "")
	assert.NotContains(t, withoutStringLiteral, "repoInterfaces.",
		"every real selector in a var/type spec must be rewritten")
}

// TestApplyOnExpr_KeyValueFieldNamesAreNotRewritten pins the exception called
// out in the walker: a struct literal's key is a FIELD NAME, not a reference.
// Qualifying it would emit `dtos.SortOrientation: v`, which does not compile.
func TestApplyOnExpr_KeyValueFieldNamesAreNotRewritten(t *testing.T) {
	src := `package services

import repoInterfaces "example.com/myapp/app/repositories/interfaces"

type Opts struct {
	UserRepositoryInterface int
}

func Build() Opts {
	_ = repoInterfaces.ErrUserNotDeletable
	return Opts{UserRepositoryInterface: 1}
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	require.NoError(t, err)

	assert.Contains(t, string(got), "UserRepositoryInterface: 1",
		"a struct-literal field name must be left alone")
}
