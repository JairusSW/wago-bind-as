package bench

import (
	_ "embed"
	"errors"
	"testing"

	bindas "github.com/JairusSW/wago-bind-as"
	request "github.com/JairusSW/wago-bind-as/examples/request"
	wago "github.com/wago-org/wago"
)

//go:embed testdata/request.wasm
var requestWasm []byte

//go:embed testdata/callback.wasm
var callbackWasm []byte

func newCall(b testing.TB, bodyBytes int) (*wago.Instance, *bindas.Prepared, bindas.Offset) {
	b.Helper()
	compiled, err := wago.Compile(nil, requestWasm)
	if err != nil {
		b.Fatal(err)
	}
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	call, err := bindas.Bind(instance, "inspect", 16<<10, 48<<10, bindas.WithoutResetZeroing())
	if err != nil {
		instance.Close()
		b.Fatal(err)
	}
	var root bindas.Offset
	err = call.WithArena(func(arena *bindas.Arena) error {
		req, err := request.NewRequest(arena)
		if err != nil {
			return err
		}
		if err = req.SetScore(2.5); err != nil {
			return err
		}
		if err = req.SetNameString(arena, "administrator"); err != nil {
			return err
		}
		if bodyBytes != 0 {
			if err = req.SetBodyBytes(arena, make([]byte, bodyBytes)); err != nil {
				return err
			}
		}
		root = req.Offset()
		return nil
	})
	if err != nil {
		instance.Close()
		b.Fatal(err)
	}
	return instance, call, root
}

func TestWagoRoundTrip(t *testing.T) {
	instance, call, root := newCall(t, 32<<10)
	defer instance.Close()
	status, err := call.Call(root)
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	err = call.WithArena(func(arena *bindas.Arena) error {
		req, err := request.OpenRequest(arena.Region(), root)
		if err != nil {
			return err
		}
		score, err := req.Score()
		if err != nil {
			return err
		}
		if score != 12.5 {
			t.Fatalf("score = %f", score)
		}
		body, err := req.Body()
		if err != nil {
			return err
		}
		if body.Len() != 32<<10 {
			t.Fatalf("body length = %d", body.Len())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWagoGuestStorageRoundTripAndExpiry(t *testing.T) {
	var escaped bindas.Region
	readRecord := wago.HostFunc(func(module wago.HostModule, params, results []uint64) {
		err := bindas.WithGuestRegion(module, 0, params[0], params[1], false, func(region bindas.Region) error {
			escaped = region
			record, err := region.Record(0, 8, 8)
			if err != nil {
				return err
			}
			value, err := record.Uint64(0)
			if err != nil {
				return err
			}
			if value != 0x0102030405060708 {
				t.Fatalf("value = %#x", value)
			}
			return nil
		})
		if err != nil {
			t.Error(err)
			results[0] = wago.I32(-1)
			return
		}
		results[0] = wago.I32(1)
	})
	compiled, err := wago.Compile(nil, callbackWasm)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{"host.read_record": readRecord}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if wago.AsI32(result[0]) != 1 {
		t.Fatalf("result = %d", wago.AsI32(result[0]))
	}
	if _, err := escaped.Record(0, 8, 8); !errors.Is(err, bindas.ErrStale) {
		t.Fatalf("escaped region error = %v", err)
	}
}

func BenchmarkWagoBindAS(b *testing.B) {
	for _, test := range []struct {
		name string
		body int
	}{{"empty-body", 0}, {"32KiB-untouched-body", 32 << 10}} {
		b.Run(test.name, func(b *testing.B) {
			instance, call, root := newCall(b, test.body)
			defer instance.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := call.Call(root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWagoPreparedFloor(b *testing.B) {
	instance, call, root := newCall(b, 0)
	defer instance.Close()
	_ = call
	function, err := instance.PrepareFunction("inspect")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := function.Invoke3(16<<10, 48<<10, uint64(root)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildNativeInput(b *testing.B) {
	for _, test := range []struct {
		name string
		body int
	}{{"empty-body", 0}, {"32KiB-body-copy", 32 << 10}} {
		b.Run(test.name, func(b *testing.B) {
			arena, err := bindas.NewArena(make([]byte, 64<<10), 0, 64<<10, bindas.WithoutResetZeroing())
			if err != nil {
				b.Fatal(err)
			}
			input := request.RequestInput{Id: 42, Score: 2.5, Name: "administrator", Body: make([]byte, test.body), Headers: []request.HeaderInput{{Name: "content-type", Value: "application/json"}}, User: &request.UserInput{Id: 9, Name: "Jairus"}}
			b.ReportAllocs()
			b.SetBytes(int64(test.body))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				arena.Reset()
				if _, err := request.BuildRequest(arena, input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
