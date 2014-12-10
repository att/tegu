// vi: sw=4 ts=4:

/*

	Mnemonic:	mbox
	Abstract:	"object" that represents a middle box for a steering reservation.
	Date:		24 June 2014
	Author:		E. Scott Daniels

	Mods:		
*/

package gizmos

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	//"os"
	//"strings"
	//"time"

	//"codecloud.web.att.com/gopkgs/clike"
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
