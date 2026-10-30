package platform

import (
	"github.com/micro/micro/v3/plugin/s3"
	"testing"
)

func TestBlobKeyFormatByServiceRole(t *testing.T) {
	for _, service := range []string{"store", "runtime"} {
		t.Run(service, func(t *testing.T) {
			options := s3.Options{}
			for _, option := range blobStoreOptions(service) {
				option(&options)
			}
			if options.UseLegacyKeys != (service == "store") {
				t.Fatalf("unexpected key format for %s", service)
			}
		})
	}
}
