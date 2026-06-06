package domain_test

import (
	"context"
	"fmt"

	"go.klarlabs.de/axi/domain"
)

// simpleInvoker wires capability names to closures; for examples only.
type simpleInvoker map[domain.CapabilityName]func(any) (any, error)

func (s simpleInvoker) Invoke(name domain.CapabilityName, input any) (any, error) {
	fn, ok := s[name]
	if !ok {
		return nil, fmt.Errorf("capability %q not registered", name)
	}
	return fn(input)
}

func ExamplePipeline() {
	pipeline := domain.NewPipeline("upper", "exclaim")
	invoker := simpleInvoker{
		"upper": func(in any) (any, error) {
			s := in.(string)
			out := make([]byte, len(s))
			for i := 0; i < len(s); i++ {
				c := s[i]
				if c >= 'a' && c <= 'z' {
					c -= 32
				}
				out[i] = c
			}
			return string(out), nil
		},
		"exclaim": func(in any) (any, error) {
			return in.(string) + "!", nil
		},
	}

	result, err := pipeline.ExecuteWithInvoker(context.Background(), "hello", invoker)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println(result)
	// Output:
	// HELLO!
}
