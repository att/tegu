// vi: sw=4 ts=4:

/*

	Mnemonic:	spq
	Abstract:	A simple object that contains a switch (id/name), port and queue number. 
				All are externally accessible and other than the constructor there are
				no functions that operate on this object.

	Date:		18 February 2013
	Author:		E. Scott Daniels

*/

package gizmos

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	//"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	//"os"
	//"strings"
	//"time"

	//"forge.research.att.com/gopkgs/clike"
)

type Spq struct {
	Switch	string
	Port	int
	Queuenum int
}



// ---------------------------------------------------------------------------------------

/*
	Creates an empty path representation between two hosts.
*/
func Mk_spq( sw string, p int, q int ) (s *Spq) {
	s = &Spq {
		Switch: sw, 
		Port:	p,
		Queuenum: q,
	}

	return
}

