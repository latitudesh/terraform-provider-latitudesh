<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->