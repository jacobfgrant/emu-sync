package storage

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"typed NoSuchKey", &types.NoSuchKey{}, true},
		{"wrapped NoSuchKey", fmt.Errorf("get: %w", &types.NoSuchKey{}), true},
		{"generic NotFound", &smithy.GenericAPIError{Code: "NotFound"}, true},
		{"NoSuchBucket", &smithy.GenericAPIError{Code: "NoSuchBucket"}, false},
		{"AccessDenied", &smithy.GenericAPIError{Code: "AccessDenied"}, false},
		{"network error", errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.want {
				t.Errorf("isNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
