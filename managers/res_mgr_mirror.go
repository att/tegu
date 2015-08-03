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
	Mnemonic:	res_mgr_mirror
	Abstract:	Reservation manager functions that are directly related to mirroring.

	Author:		Robert Eby

	Mods:		23 Feb 2015 : Created.
				26 May 2015 - Changes to support pledge as an interface.
*/

package managers

import (
	"fmt"
	"strings"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
 *	Push an "add mirror" request out to an agent in order to create the mirror.
 */
func push_mirror_reservation( gp *gizmos.Pledge, rname string, ch chan *ipc.Chmsg ) {

	p, ok := (*gp).( *gizmos.Pledge_mirror )		// better be a mirroring pledge
	if ! ok {
		rm_sheep.Baa( 1, "internal error: pledge passed to push_mirror_reservations wasn't a mirror pledge" )
		(*gp).Set_pushed() 						// prevent looping until it expires
		return
	}

	ports, out, _, _, _, _, _, _ := p.Get_values( )
	ports2 := strings.Replace(*ports, " ", ",", -1)	// ports must be comma separated
	id := p.Get_id( )
	host := p.Get_qid( )
	rm_sheep.Baa( 1, "Adding mirror %s on host %s", *id, *host )
	json := `{ "ctype": "action_list", "actions": [ { `
	json += `"atype": "mirrorwiz", `
	json += fmt.Sprintf(`"hosts": [ %q ], `,  *host)
	if strings.Contains(ports2, ",vlan:") {
		// Because we have to store the ports list and the vlans in the same field
		// we split it out here
		n := strings.Index(ports2, ",vlan:")
		vlan := ports2[n+6:]
		ports2 = ports2[:n]
		json += fmt.Sprintf(`"qdata": [ "add", %q, %q, %q, %q ] `, *id, ports2, *out, vlan)
	} else {
		json += fmt.Sprintf(`"qdata": [ "add", %q, %q, %q ] `, *id, ports2, *out)
	}
	json += `} ] }`
	rm_sheep.Baa( 2, " JSON -> %s", json )
	msg := ipc.Mk_chmsg( )
	msg.Send_req( am_ch, nil, REQ_SENDSHORT, json, nil )		// send this as a short request to one agent	
	p.Set_pushed()
}

/*
 *	Push a "delete mirror" request out to an agent in order to remove the mirror.
 */
func undo_mirror_reservation( gp *gizmos.Pledge, rname string, ch chan *ipc.Chmsg ) {

	p, ok := (*gp).( *gizmos.Pledge_mirror )		// better be a mirroring pledge
	if ! ok {
		rm_sheep.Baa( 1, "internal error: pledge passed to undo_mirror_reservations wasn't a mirror pledge" )
		(*gp).Set_pushed() 						// prevent looping until it expires
		return
	}

	id := p.Get_id( )
	host := p.Get_qid( )
	rm_sheep.Baa( 1, "Deleting mirror %s on host %s", *id, *host )
	json := `{ "ctype": "action_list", "actions": [ { `
	json += `"atype": "mirrorwiz", `
	json += fmt.Sprintf(`"hosts": [ %q ], `,  *host)
	json += fmt.Sprintf(`"qdata": [ "del", %q ] `, *id)
	json += `} ] }`
	rm_sheep.Baa( 2, " JSON -> %s", json )
	msg := ipc.Mk_chmsg( )
	msg.Send_req( am_ch, nil, REQ_SENDSHORT, json, nil )		// send this as a short request to one agent	
	p.Set_pushed()
}
