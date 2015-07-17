// vi: sw=4 ts=4:

/*

	Mnemonic:	spq
	Abstract:	A simple object that contains a switch (id/name), port and queue number. 
				All are externally accessible and other than the constructor there are
				no functions that operate on this object.

	Date:		18 February 2013
	Author:		E. Scott Daniels
	Mod:		11 Jun 2015 - corrected comment, removed uneeded import commented things.

*/

package gizmos

import (
	"fmt"

	//"codecloud.web.att.com/gopkgs/clike"
)

type Spq struct {
	Switch	string
	Port	int
	Queuenum int
}



// ---------------------------------------------------------------------------------------

/*
	Creates a switch/port/queue representation for an endpoint.
*/
func Mk_spq( sw string, p int, q int ) (s *Spq) {
	s = &Spq {
		Switch: sw, 
		Port:	p,
		Queuenum: q,
	}

	return
}

func (s *Spq) String( ) ( string ) {
	if s == nil {
		return "==nil=="
	}

	return fmt.Sprintf( "spq: %s %d %d", s.Switch, s.Port, s.Queuenum )
}
