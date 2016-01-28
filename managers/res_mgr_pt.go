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

	Mnemonic:	res_mgr_pt
	Abstract:	Functions which apply only to the passthrough reservations.

	Date:		26 January 2016
	Author:		E. Scott Daniels

	Mods:		
*/

package managers

import (
	"time"

	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
	For a single passthrough pledge, this function sets things up and sends needed requests to the fq-manger to
	create any necessary flow-mods.

	We send the following information to fq_mgr:
		source mac or endpoint	(VM-- the host in the pledge)
		source IP and optionally port and protocol more specific reservations
		expiry
		switch	(physical host -- compute node)
	
	Errors are returned to res_mgr via channel, but asycnh; we do not wait for responses to each message
	generated here.

	To_limit is a cap to the expiration time sent when creating a flow-mod.  OVS (and others we assume)
	use an unsigned int32 as a hard timeout value, and thus have an upper limit of just over 18 hours. If
	to_limit is > 0, we'll ensure that the timeout passed on the request to fq-mgr won't exceed  the limit,
 	and we assume that this function is called periodically to update long running reservations.
*/
func pass_push_res( gp *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, to_limit int64 ) {
	var (
		msg		*ipc.Chmsg
	)

	now := time.Now().Unix()

	p, ok :=  (*gp).( *gizmos.Pledge_pass )		// generic pledge better be a passthrough pledge!
	if ! ok {
		rm_sheep.Baa( 1, "internal error in pass_push_reservation: pledge isn't a passthrough pledge" )
		(*gp).Set_pushed()						// prevent looping
		return
	}

	host, _,  _, expiry, proto := p.Get_values( )			// reservation info that we need
	//v1, v2 := p.Get_vlan( )								// vlan match criteria for one/both endpoints

	ip := name2ip( host )
rm_sheep.Baa( 1, ">>>>> host name %s converted to ip address for freq", *host, *ip )

	if ip != nil {											// good ip addresses so we're good to go
		freq := Mk_fqreq( rname )						// default flow mod request with empty match/actions (for bw requests, we don't need priority or such things)
		freq.Match.Smac = ip							// fq_mgr has conversion map to convert to mac
		freq.Swid = p.Get_phost()						// the phyiscal host where the VM lives and where fmods need to be deposited

		freq.Cookie = 0xffff							// should be ignored, if we see this out there we've got problems

		if (*p).Is_paused( ) {
			freq.Expiry = time.Now().Unix( ) +  15		// if reservation shows paused, then we set the expiration to 15s from now  which should force the flow-mods out
		} else {
			if to_limit > 0 && expiry > now + to_limit {
				freq.Expiry = now + to_limit			// expiry must be capped so as not to overflow virtual switch variable size
			} else {
				freq.Expiry = expiry
			}
		}
		freq.Id = rname

		freq.Extip = &empty_str

														// this will change when ported to endpoint branch as the endpoint allows address and port 'in line'
		freq.Match.Ip1 = proto							// the proto on the reservation should be [{udp|tcp:}]address[:port]
		freq.Match.Ip2 = nil
		freq.Espq =   nil
		dup_str := ""
		freq.Exttyp = &dup_str

		rm_sheep.Baa( 1, "pushing passthru reservation: %s", p )
		msg = ipc.Mk_chmsg()
		msg.Send_req( fq_ch, ch, REQ_PT_RESERVE, freq, nil )					// queue work with fq-manger to read the struct and send cmd(s) to agent to get it done

		p.Set_pushed()				// safe to mark the pledge as having been pushed.
	}
}
