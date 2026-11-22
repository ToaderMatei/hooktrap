// Package listeners holds the optional non-HTTP capture listeners (DNS, raw TCP).
package listeners

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

// Recorder persists a captured interaction.
type Recorder interface {
	Record(r model.Request) (model.Request, error)
}

// DNSServer is a minimal UDP DNS responder that logs every query. It answers A
// queries with a fixed IP, which makes out-of-band DNS interactions (a common
// CTF exfil/SSRF signal) visible on the dashboard. It is intentionally tiny and
// not a general-purpose resolver.
type DNSServer struct {
	addr     string
	answerIP net.IP
	rec      Recorder
	log      *slog.Logger
	conn     *net.UDPConn
}

// NewDNSServer builds a DNS listener bound to addr (host:port) answering with answerIP.
func NewDNSServer(addr, answerIP string, rec Recorder, log *slog.Logger) (*DNSServer, error) {
	ip := net.ParseIP(answerIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("dns answer ip %q must be IPv4", answerIP)
	}
	return &DNSServer{addr: addr, answerIP: ip, rec: rec, log: log}, nil
}

// Start begins serving until ctx is cancelled.
func (d *DNSServer) Start(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp", d.addr)
	if err != nil {
		return fmt.Errorf("resolve dns addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen dns: %w", err)
	}
	d.conn = conn
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	buf := make([]byte, 512)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		d.handle(append([]byte(nil), buf[:n]...), remote)
	}
}

func (d *DNSServer) handle(query []byte, remote *net.UDPAddr) {
	name, qtype, ok := parseQuestion(query)
	if !ok {
		return
	}
	req := model.Request{
		BinID:      store.DefaultBinID,
		Kind:       model.KindDNS,
		Time:       time.Now(),
		RemoteAddr: remote.IP.String(),
		Path:       name,
		Summary:    fmt.Sprintf("DNS %s %s", dnsType(qtype), name),
		Body:       []byte(fmt.Sprintf("query %s type %s from %s", name, dnsType(qtype), remote)),
	}
	req.BodySize = len(req.Body)
	if _, err := d.rec.Record(req); err != nil {
		d.log.Error("record dns", "err", err)
	}
	if resp, err := buildAnswer(query, qtype, d.answerIP); err == nil {
		_, _ = d.conn.WriteToUDP(resp, remote)
	}
}

// parseQuestion extracts the QNAME and QTYPE from a DNS query message.
func parseQuestion(msg []byte) (name string, qtype uint16, ok bool) {
	if len(msg) < 12 {
		return "", 0, false
	}
	pos := 12
	var labels []string
	for pos < len(msg) {
		l := int(msg[pos])
		pos++
		if l == 0 {
			break
		}
		if l > 63 || pos+l > len(msg) {
			return "", 0, false
		}
		labels = append(labels, string(msg[pos:pos+l]))
		pos += l
	}
	if pos+4 > len(msg) {
		return "", 0, false
	}
	qtype = uint16(msg[pos])<<8 | uint16(msg[pos+1])
	return strings.Join(labels, "."), qtype, true
}

// buildAnswer constructs a response: an A answer for A queries, otherwise an
// empty NOERROR echoing the question.
func buildAnswer(query []byte, qtype uint16, ip net.IP) ([]byte, error) {
	if len(query) < 12 {
		return nil, fmt.Errorf("short query")
	}
	questionEnd := 12
	for questionEnd < len(query) {
		l := int(query[questionEnd])
		questionEnd++
		if l == 0 {
			break
		}
		questionEnd += l
	}
	questionEnd += 4
	if questionEnd > len(query) {
		return nil, fmt.Errorf("bad question")
	}
	resp := make([]byte, questionEnd)
	copy(resp, query[:questionEnd])
	resp[2] = 0x84 // QR=1, AA=1
	resp[3] = 0x00
	resp[6], resp[7] = 0x00, 0x00 // ancount set below
	resp[8], resp[9] = 0x00, 0x00
	resp[10], resp[11] = 0x00, 0x00
	if qtype != 1 {
		return resp, nil
	}
	resp[7] = 0x01
	answer := []byte{
		0xc0, 0x0c, // pointer to question name
		0x00, 0x01, // type A
		0x00, 0x01, // class IN
		0x00, 0x00, 0x00, 0x1e, // TTL 30
		0x00, 0x04, // rdlength
		ip[0], ip[1], ip[2], ip[3],
	}
	return append(resp, answer...), nil
}

func dnsType(t uint16) string {
	switch t {
	case 1:
		return "A"
	case 2:
		return "NS"
	case 5:
		return "CNAME"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 28:
		return "AAAA"
	case 255:
		return "ANY"
	}
	return fmt.Sprintf("TYPE%d", t)
}
