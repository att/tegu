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
	Mnemonic:	res_mgr_chain
	Abstract:	Reservation manager functions that are directly related to chaining (SFS).

	Author:		Robert Eby

	Mods:		01 Sep 2015 : Created.
*/

package managers

import (
	"fmt"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
	Push an active, unpushed chain request out to the nodes.  This causes the chain's "plan" file
	to be executed, with the 'start' argument, on those nodes that are affected.
 */
func push_sfs_chain_reservation( gp *gizmos.Pledge, ch chan *ipc.Chmsg ) {
	common_chain_res(gp, ch, "start")
}

/*
	Send a request to the nodes to remove an active chain.  This causes the chain's "plan" file
	to be executed, with the 'stop' argument, on those nodes that are affected.
 */
func undo_sfs_chain_reservation( gp *gizmos.Pledge, ch chan *ipc.Chmsg ) {
	common_chain_res(gp, ch, "stop")
}

func common_chain_res( gp *gizmos.Pledge, ch chan *ipc.Chmsg, action string ) {
	p, ok := (*gp).( *gizmos.Chain )		// better be a chain pledge
	if ! ok {
		rm_sheep.Baa( 1, "internal error: pledge passed to *_sfs_chain_reservation wasn't a chain pledge" )
		(*gp).Set_pushed() 						// prevent looping until it expires
		return
	}
	planfile, err := p.GetPlanFile()
	if err != nil {
		rm_sheep.Baa( 1, "sfs: plan file error: %s, %s", planfile, err.Error() )
		(*gp).Set_pushed() 						// prevent looping until it expires
		return
	}
	hosts := p.GetPlanHosts()
	rm_sheep.Baa( 1, "sfs: pushing chain %s to hosts %v", *p.Get_id(), hosts )

	json := `{ "ctype": "action_list", "actions": [ { "atype": "run_local_cmd", "hosts": [`
	sep := ""
	for _, h := range hosts {
		json += fmt.Sprintf("%s %q", sep, h)
		sep = ","
	}
	json += ` ], `
	json += fmt.Sprintf(`"qdata": [ %q, %q ] `, planfile, action )
	json += `} ] }`
	rm_sheep.Baa( 1, " JSON -> %s", json )

	msg := ipc.Mk_chmsg( )
	msg.Send_req( am_ch, nil, REQ_SENDSHORT, json, nil )		// send this as a short request to one agent
	p.Set_pushed()
}
