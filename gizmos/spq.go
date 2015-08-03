// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


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

	//"github.com/att/att/gopkgs/clike"
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
