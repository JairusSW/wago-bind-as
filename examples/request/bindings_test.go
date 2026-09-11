package request

import (
	"errors"
	"testing"

	bindas "github.com/JairusSW/wago-bind-as"
)

func TestGeneratedTypedVectorAccess(t *testing.T) {
	arena, err := bindas.NewArena(make([]byte, 2048), 0, 2048)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(arena)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := request.ReserveHeaders(arena, 4); err != nil {
		t.Fatal(err)
	}
	header, err := request.AppendHeaders()
	if err != nil {
		t.Fatal(err)
	}
	if err := header.SetNameString(arena, "content-type"); err != nil {
		t.Fatal(err)
	}
	if err := header.SetValueString(arena, "application/json"); err != nil {
		t.Fatal(err)
	}
	got, err := request.HeadersAt(0)
	if err != nil {
		t.Fatal(err)
	}
	name, err := got.NameUTF8()
	if err != nil {
		t.Fatal(err)
	}
	if !name.EqualString("content-type") {
		t.Fatal("generated vector element does not alias the shared bytes")
	}
}

func TestGeneratedViewsPreserveRegionProvenance(t *testing.T) {
	first, _ := bindas.NewArena(make([]byte, 1024), 0, 1024)
	second, _ := bindas.NewArena(make([]byte, 1024), 0, 1024)
	request, err := BuildRequest(first, RequestInput{User: &UserInput{Name: "first"}})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := BuildUser(second, UserInput{Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := request.SetUser(foreign); !errors.Is(err, bindas.ErrRegionMismatch) {
		t.Fatalf("cross-region reference error = %v", err)
	}
	if err := request.SetNameString(second, "wrong"); !errors.Is(err, bindas.ErrRegionMismatch) {
		t.Fatalf("cross-region string error = %v", err)
	}
	if _, err := request.ReserveHeaders(second, 1); !errors.Is(err, bindas.ErrRegionMismatch) {
		t.Fatalf("cross-region vector error = %v", err)
	}
	first.Reset()
	if _, err := request.User(); !errors.Is(err, bindas.ErrStale) {
		t.Fatalf("stale child error = %v", err)
	}
}

func TestGeneratedNativeInputBuilder(t *testing.T) {
	arena, _ := bindas.NewArena(make([]byte, 4096), 0, 4096)
	request, err := BuildRequest(arena, RequestInput{
		Id: 42, Score: 2.5, Flags: 7, Name: "admin", Body: []byte("cold payload"),
		Headers: []HeaderInput{{Name: "content-type", Value: "application/json"}},
		User:    &UserInput{Id: 9, Name: "Jairus"}, Timestamp: 123,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := request.Id(); id != 42 {
		t.Fatalf("id = %d", id)
	}
	if body, _ := request.Body(); body.Len() != uint32(len("cold payload")) {
		t.Fatalf("body length = %d", body.Len())
	}
	header, err := request.HeadersAt(0)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := header.ValueUTF8()
	if !value.EqualString("application/json") {
		t.Fatal("header value mismatch")
	}
	user, err := request.User()
	if err != nil {
		t.Fatal(err)
	}
	userName, _ := user.NameUTF8()
	if !userName.EqualString("Jairus") {
		t.Fatal("user name mismatch")
	}
}

func TestGeneratedBuilderRollsBackOnFailure(t *testing.T) {
	arena, _ := bindas.NewArena(make([]byte, 128), 0, 128)
	before := arena.Used()
	_, err := BuildRequest(arena, RequestInput{Name: "admin", Body: make([]byte, 256), User: &UserInput{}})
	if err == nil {
		t.Fatal("expected out-of-space error")
	}
	if arena.Used() != before {
		t.Fatalf("used bytes after failed build = %d, want %d", arena.Used(), before)
	}
}
