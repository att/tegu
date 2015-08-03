
	
package managers_test

import "testing"
import "fmt"
import "os"


func TestMan_util( t *testing.T ) {
	str = "udp:42"
	pv, port = managers.proto2val_port( &str )
	fmt.Fprintf( os.Stderr, "%d %d\n", pv, port )
}

