package trusted

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
)

const (
	xff       string = "X-Forwarded-For"
	xrip      string = "X-Real-Ip"
	tcip      string = "True-Client-Ip"
	cfip      string = "Cf-Connecting-Ip"
	forwarded string = "Forwarded"
)

var (
	forwardedRegex = regexp.MustCompile(`(?i)(?:for=)([^(;|,| )]+)`)
)

func NewTrustedResolver(trusted []*net.IPNet) *Trusted {
	return &Trusted{
		trusted: trusted,
	}
}

type Trusted struct {
	trusted []*net.IPNet
}

func (t *Trusted) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := t.resolveIP(r.Header)
		if ip == "" {
			var err error
			ip, _, err = net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		if !t.isTrusted(ip) {
			http.Error(w, fmt.Sprintf("ip address is not trusted: %s", ip), http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (t *Trusted) isTrusted(ip string) bool {
	// if user doesn't specify trusted addresses, all addresses are trusted
	if len(t.trusted) == 0 {
		// add addr to the cache
		return true
	}

	i := net.ParseIP(ip)
	if i == nil {
		return false
	}

	for _, cird := range t.trusted {
		if cird.Contains(i) {
			return true
		}
	}

	return false
}

// get real ip passing multiple proxy
func (t *Trusted) resolveIP(headers http.Header) string {
	if fwd := headers.Get(xff); fwd != "" {
		s := strings.Index(fwd, ", ")
		if s == -1 {
			return fwd
		}

		if len(fwd) < s {
			return ""
		}

		return fwd[:s]
		// next -> X-Real-Ip
	} else if fwd := headers.Get(xrip); fwd != "" {
		return fwd
		// new Forwarded header
		//https://datatracker.ietf.org/doc/html/rfc7239
	} else if fwd := headers.Get(forwarded); fwd != "" {
		if get := forwardedRegex.FindStringSubmatch(fwd); len(get) > 1 {
			// IPv6 -> It is important to note that an IPv6 address and any nodename with
			// node-port specified MUST be quoted
			// we should trim the "
			return strings.Trim(get[1], `"`)
		}
	}

	// The logic here is the following:
	// CloudFlare headers
	// True-Client-IP is a general CF header in which copied information from X-Real-Ip in CF.
	// CF-Connecting-IP is an Enterprise feature and we check it last in order.
	// This operations are near O(1) because Headers struct are the map type -> type MIMEHeader map[string][]string
	if fwd := headers.Get(tcip); fwd != "" {
		return fwd
	}

	if fwd := headers.Get(cfip); fwd != "" {
		return fwd
	}

	return ""
}
