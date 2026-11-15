//go:build linux

package sandbox

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

// bringLoopbackUp ensures the loopback interface is up inside the current
// network namespace. Bubblewrap with --unshare-net configures lo (127.0.0.1/8
// and IFF_UP) before dropping capabilities, so unprivileged sandboxed children
// lack CAP_NET_ADMIN for RTM_SETLINK; return early when lo is already up. When
// lo is still down (e.g. a bare unshared namespace), raise it with a netlink
// RTM_SETLINK request so the relay can bind 127.0.0.1 without iproute2.
func bringLoopbackUp() error {
	if iface, err := net.InterfaceByName("lo"); err == nil && iface.Flags&net.FlagUp != 0 {
		return nil
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("sandbox: open netlink socket: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("sandbox: bind netlink socket: %w", err)
	}
	native := nativeEndian()

	// ifinfomsg (ifi_family, pad, ifi_type, ifi_index, ifi_flags, ifi_change)
	// followed by the IFLA_IFNAME attribute naming the interface. Index 0
	// tells the kernel to resolve the interface by name.
	var ifinfo [16]byte
	ifinfo[0] = unix.AF_UNSPEC
	native.PutUint16(ifinfo[2:4], unix.ARPHRD_LOOPBACK)
	native.PutUint32(ifinfo[8:12], unix.IFF_UP)
	native.PutUint32(ifinfo[12:16], unix.IFF_UP)

	name := append([]byte("lo"), 0)
	attrLen := 4 + len(name)
	attr := make([]byte, (attrLen+3)&^3)
	native.PutUint16(attr[0:2], uint16(attrLen))
	native.PutUint16(attr[2:4], unix.IFLA_IFNAME)
	copy(attr[4:], name)

	body := append(append([]byte(nil), ifinfo[:]...), attr...)
	padded := (unix.NLMSG_HDRLEN + len(body) + 3) &^ 3
	req := make([]byte, padded)
	native.PutUint32(req[0:4], uint32(padded))
	native.PutUint16(req[4:6], unix.RTM_SETLINK)
	native.PutUint16(req[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	native.PutUint32(req[8:12], 1)
	copy(req[unix.NLMSG_HDRLEN:], body)

	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("sandbox: send RTM_SETLINK: %w", err)
	}

	// The kernel answers with NLMSG_ERROR; errno 0 is the ack for success.
	buf := make([]byte, 4096)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		return fmt.Errorf("sandbox: read netlink ack: %w", err)
	}
	if n < unix.NLMSG_HDRLEN+4 {
		return io.ErrUnexpectedEOF
	}
	if msgType := native.Uint16(buf[4:6]); msgType != unix.NLMSG_ERROR {
		return fmt.Errorf("sandbox: unexpected netlink reply type %d", msgType)
	}
	if errno := int32(native.Uint32(buf[unix.NLMSG_HDRLEN:])); errno != 0 {
		return fmt.Errorf("sandbox: bring loopback up: %w", unix.Errno(-errno))
	}
	return nil
}

// nativeEndian reports the host byte order used by netlink messages.
func nativeEndian() binary.ByteOrder {
	var probe uint16 = 0x0102
	if *(*byte)(unsafe.Pointer(&probe)) == 0x02 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}
