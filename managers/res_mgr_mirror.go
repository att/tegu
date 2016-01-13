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

	Mods:		23 Feb 2015 - Created.
				26 May 2015 - Changes to support pledge as an interface.
				16 Nov 2015 - Add save_mirror_response()
				24 Nov 2015 - Add options
*/

package managers

import (
	"fmt"
	"regexp"
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

	// This is somewhat of a hack, but as long as the code in tegu_agent:do_mirrorwiz doesn't change, it should work
	id := p.Get_id( )
	arg := *id
	opts := p.Get_Options()
	if opts != nil && *opts != "" {
		arg = fmt.Sprintf("-o%s %s", *opts, *id)
	}

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
		json += fmt.Sprintf(`"qdata": [ "add", %q, %q, %q, %q ] `, arg, ports2, *out, vlan)
	} else {
		json += fmt.Sprintf(`"qdata": [ "add", %q, %q, %q ] `, arg, ports2, *out)
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
	// This is somewhat of a hack, but as long as the code in tegu_agent:do_mirrorwiz doesn't change, it should work
	arg := *id
	opts := p.Get_Options()
	if opts != nil && *opts != "" {
		arg = fmt.Sprintf("-o%s %s", *opts, *id)
	}

	host := p.Get_qid( )
	rm_sheep.Baa( 1, "Deleting mirror %s on host %s", *id, *host )
	json := `{ "ctype": "action_list", "actions": [ { `
	json += `"atype": "mirrorwiz", `
	json += fmt.Sprintf(`"hosts": [ %q ], `,  *host)
	json += fmt.Sprintf(`"qdata": [ "del", %q ] `, arg)
	json += `} ] }`
	rm_sheep.Baa( 2, " JSON -> %s", json )
	msg := ipc.Mk_chmsg( )
	msg.Send_req( am_ch, nil, REQ_SENDSHORT, json, nil )		// send this as a short request to one agent	
	p.Set_pushed()
}

/*
 *  Save the returned response from an agent into the mirror pledge.  This is a gigantic hack,
 *  but the present design of the agents doesn't provide an easy way to do this.
 */
func save_mirror_response( stdout []string, stderr []string ) {
	// Try to figure out which mirror these responses are for.
	// Look for a valid mirror name in any output
	re := regexp.MustCompile(`mir-[0-9a-f]{8}_[0-9]`)
	for _, s := range append(stdout, stderr...) {
		name := re.FindString(s)
		if name != "" {
			// Fetch the mirror and save the stdout/err
			m := lookupMirror( name, *super_cookie )
			if m != nil {
				rm_sheep.Baa( 1, "Saving output for mirror %s", name )
				m.Set_Output( stdout, stderr )
			}
			return
		}
	}
	rm_sheep.Baa( 1, "save_mirror_response: could not find the mirror name" )
}
