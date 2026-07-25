package videocollector

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type resolverStub struct {
	addresses []net.IPAddr
	err       error
}

func (r resolverStub) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, r.err
}

func TestValidatePublicMediaURL(t *testing.T) {
	resolver := resolverStub{addresses: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	got, err := ValidatePublicMediaURL(context.Background(), "https://www.tiktok.com/@user/video/123", resolver)

	require.NoError(t, err)
	require.Equal(t, "https", got.Scheme)
	require.Equal(t, "www.tiktok.com", got.Hostname())
}

func TestValidatePublicMediaURLRejectsUnsafeTargets(t *testing.T) {
	publicResolver := resolverStub{addresses: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	privateResolver := resolverStub{addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}}}

	tests := []struct {
		name     string
		value    string
		resolver IPResolver
	}{
		{name: "file scheme", value: "file:///etc/passwd", resolver: publicResolver},
		{name: "credentials", value: "https://user:pass@example.com/video", resolver: publicResolver},
		{name: "localhost", value: "http://localhost/video", resolver: publicResolver},
		{name: "private ipv4", value: "http://192.168.1.2/video", resolver: publicResolver},
		{name: "private ipv6", value: "http://[::1]/video", resolver: publicResolver},
		{name: "private dns result", value: "https://media.example.com/video", resolver: privateResolver},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePublicMediaURL(context.Background(), tt.value, tt.resolver)
			require.Error(t, err)
		})
	}
}
