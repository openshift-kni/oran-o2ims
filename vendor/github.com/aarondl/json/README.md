# json

Fork of the official JSON package with a single addition of an interface that
allows custom types to decide if they should be omitted or not when `omitempty`
is in the struct tag.

Implement the following interface to be able to omit a field of a custom type
from the output:

```go
type isZeroer interface {
	MarshalJSONIsZero() bool
}
```

### Example

```go
package main

import (
	"fmt"

	"github.com/aarondl/json"
)

type OmitMe struct{}

func (OmitMe) MarshalJSONIsZero() bool { return true }
func (o OmitMe) MarshalJSON() ([]byte, error) {
	return []byte(`5`), nil
}

func main() {
	a := struct {
		NotOmitted OmitMe
		Omitted    OmitMe `json:",omitempty"`
	}{}

	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", b)
	// {
	//    "NotOmitted": 5
	// }
}
```
