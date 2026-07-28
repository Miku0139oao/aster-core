package dns

// export functions from tunnel module

import "github.com/Miku0139oao/aster-core/tunnel"

const RespectRules = tunnel.DnsRespectRules

type dnsDialer = tunnel.DNSDialer

var newDNSDialer = tunnel.NewDNSDialer
