package dedicated

import (
	"github.com/micro/micro/v3/internal/selector"
	"math/rand"
)

type dedicated struct{
	uid string
}

func (r *dedicated) Select(routes []string, opts ...selector.SelectOption) (selector.Next, error) {
	// we can't select from an empty pool of routes
	if len(routes) == 0 {
		return nil, selector.ErrNoneAvailable
	}

	// return the next func
	return func() string {
		// if there is only one route provided we'll select it
		if len(routes) == 1 {
			return routes[0]
		}

		// select a random route from the slice
		return routes[rand.Intn(len(routes)-1)]
	}, nil
}

func (r *dedicated) Record(addr string, err error) error {
	return nil
}

func (r *dedicated) Reset() error {
	return nil
}

func (r *dedicated) String() string {
	return "random"
}

// NewSelector returns a random selector
func NewSelector(opts ...selector.Option) selector.Selector {
	return new(dedicated)
}

func Wrapper()  {

}