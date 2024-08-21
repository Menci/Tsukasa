package main

import (
	"io"
	"net"
	"sync/atomic"
)

func PipeAndClose(conn net.Conn, targetConn net.Conn, logger *Logger) {
	var closed uint32 = 0
	close := func() {
		if atomic.CompareAndSwapUint32(&closed, 0, 1) {
			conn.Close()
			targetConn.Close()
		}
	}

	// Forward data between conn and targetConn.
	go func() {
		defer close()

		_, err := io.Copy(targetConn, conn)
		if err != nil && atomic.LoadUint32(&closed) == 0 {
			logger.Errorf("error copying data to target: %v\n", err)
		}
		conn.Close()
	}()

	defer close()

	if _, err := io.Copy(conn, targetConn); err != nil && atomic.LoadUint32(&closed) == 0 {
		logger.Errorf("error copying data from target: %v\n", err)
	}
}
