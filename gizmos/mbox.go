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

	Mnemonic:	mbox
	Abstract:	"object" that represents a middle box for a steering reservation.
	Date:		24 June 2014
	Author:		E. Scott Daniels

	Mods:		
*/

package gizmos

import (
	"fmt"
)

type Mbox struct {
	id	*string					// name or id of the VM
	mac	*string					// mac address for routing to the box
	swid *string				// switch that owns the connection to the mobx (could be phost for OVS and late binding actions)
	swport	int					// port that the box is attached to (may be -128 for late binding)
}

/*
	Constructor; creates a middle box
*/
func Mk_mbox( id *string, mac *string, swid *string, swport int ) ( mb *Mbox ) {

	mb = nil

	mb = &Mbox {
		id:		id,
		mac:	mac,
		swid:	swid,
		swport:	swport,
	}

	return
}

/*
	Returns the id/name -- which ever was given when created.
*/
func (mb *Mbox) Get_id( ) ( *string ) {
	return mb.id
}

/*
	Returns the switch ID and port.
*/
func (mb *Mbox) Get_sw_port( ) ( *string, int ) {
	return mb.swid, mb.swport
}

/*
	Returns the mac
*/
func (mb *Mbox) Get_mac( ) ( *string ) {
	return mb.mac
}

/*
	Returns all information.
*/
func (mb *Mbox) Get_values( ) ( id *string, mac *string, swid *string, swport int ) {
	return mb.id, mb.mac, mb.swid, mb.swport
}

/*
	Generate a json representation.
*/
func (mb *Mbox) To_json( ) ( *string ) {
	s := fmt.Sprintf( `{ "id": %q, "mac": %q, "swid": %q, "swport": %d }`, *mb.id, *mb.mac, *mb.swid, mb.swport )
	return &s
}
