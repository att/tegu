// vi: sw=4 ts=4:

/*

	Mnemonic:	res_mgr_steer
	Abstract:	reservation manager functions that are directly related to steering
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		
*/

package managers

import (
	//"encoding/json"
	//"fmt"
	//"os"
	"strings"
	"time"

	//"codecloud.web.att.com/gopkgs/bleater"
	//"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)


/*
	Generate flow-mod requests to the fq-manager for a given src,dest pair and list of 
	middleboxes.  This assumes that the middlebox list has been reversed if necessary. 
	Either source (ep1) or dest (ep2) may be nil which indicates a "to any" or "from any"
	intention. 

	DANGER:  We generate the flowmods in reverse order which _should_ generate the highest 
			priority f-mods first. This is absolutely necessary to prevent packet loops
			on the switches which can happen if a higher priority rule isn't in place 
			that would cause the lower priority rule to be skipped over. 

	TODO: add transport layer port support
*/
func steer_fmods( ep1 *string, ep2 *string, mblist []*gizmos.Mbox, expiry int64, rname *string, proto *string, forward bool ) {
	var (
		fq_data *Fq_req
		fq_match *Fq_parms
		fq_action *Fq_parms
		mb	*gizmos.Mbox							// current middle box being worked with (must span various blocks)
	)

	if expiry < 5 {									// refuse if too short
		return
	}

	mstr := "0x00/0x02"								// meta data match string; match if mask 0x02 is not set
	nmb := len( mblist )
	for i := 0; i < nmb; i++ {						// check value of each in list and bail if any are nil
		if mblist[i] == nil {
			rm_sheep.Baa( 1, "IER: steer_fmods: unexpected nil mb i=%d nmb=%d", i,  nmb )
			return
		}
	}

	for i :=  nmb -1; i >= 0;  i-- {					// backward direction ep2->ep1
		resub := "90 0"									// we resubmit to table 90 to set our meta data and then resub to 0 to catch openstack rules

		if i == nmb - 1 {								// for last mb we need a rule that causes steering to be skipped based on mb mac
			fq_data = Mk_fqreq( rname )					// get a block and initialise to sane values
			fq_match = fq_data.Match
			fq_action = fq_data.Action

			fq_data.Pri = 300
			fq_data.Expiry = expiry

			mb = mblist[i]
			fq_match.Ip1 = ep1
			fq_match.Meta = &mstr
			/*
			fq_match = &Fq_parms{						// new structs for each since they sit in fq manages quwue
				Swport:	-1,								// port 0 is valid, so we need something that is ignored if not set later
				Meta:	&mstr,
				Ip1:  	ep1,
				Tpdport: -1,
				Tpsport: -1,
			}
			*/

			fq_action.Resub = &resub
			/*
			fq_action = &Fq_parms{
				Resub: &resub,							// resubmit to table 90 to set meta info, then to 0 to get tunnel matches
			}
			*/

			/*
			fq_data = &Fq_req {							// generate data with just what needs to be there
				Pri:	300,
				Id:		rname,
				Expiry:	expiry,
				Match:	fq_match,
				Action:	fq_action,
			}
			*/

			if proto != nil {								// set the protocol match port dest in forward direction, src in reverse
				toks := strings.Split( *proto, ":" );
				fq_data.Protocol = &toks[0]
				if forward {
					fq_data.Match.Tpdport = &toks[1]
				} else {
					fq_data.Match.Tpsport = &toks[1]
				}
			}

			if ep1 != nil {									// if source is a specific address, then we need only one 300 rule
				fq_data.Match.Ip1 = nil												// there is no source to match at this point
				fq_data.Match.Smac = nil
				fq_data.Match.Ip2 = ep2
				fq_data.Swid, fq_data.Match.Swport = mb.Get_sw_port( )	// specific switch and input port needed for this fmod
				fq_data.Lbmac = mb.Get_mac()							// fqmgr will need the mac if the port is late binding
			} else {													// if no specific src, 100 rule lives on each switch, so we must put a 300 on each too
				fq_data.Swid = nil										// force to all switches
				fq_data.Match.Smac = mb.Get_mac()						// src for 300 is the last mbox
			}

			jstr, _ := fq_data.To_json( ) 
			rm_sheep.Baa( 1, "write final fmod: %s", jstr )

			msg := ipc.Mk_chmsg()
			msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// final flow-mod from the last middlebox out
		}


		fq_data = Mk_fqreq( rname )					// get a block and initialise to sane values
		fq_match = fq_data.Match
		fq_action = fq_data.Action

		fq_match.Meta = &mstr

		/*
		fq_match = &Fq_parms{
			Swport:	-1,								// port 0 is valid, so we need something that is ignored if not set later
			Meta:	&mstr,
		}
		*/

		fq_action.Resub = &resub
		/*
		fq_action = &Fq_parms{
			Resub: &resub,							// resubmit to table 10 to set meta info, then to 0 to get tunnel matches
		}
		*/

		/*
		fq_data = &Fq_req {						// fq-mgr request data
			Id:		rname,
			Expiry:	expiry,
			Match: 	fq_match,
			Action: fq_action,
			Protocol: proto,
		}
		*/

		fq_data.Expiry = expiry
		fq_data.Protocol = proto
		fq_data.Match.Ip1 = ep1
		fq_data.Match.Ip2 = ep2

		if proto != nil {								// set the protocol match port dest in forward direction, src in reverse
			toks := strings.Split( *proto, ":" );
			fq_data.Protocol = &toks[0]
			if forward {
				fq_data.Match.Tpdport = &toks[1]
			} else {
				fq_data.Match.Tpsport = &toks[1]
			}
		}
	
		if i == 0 {									// push the ingress rule (possibly to all switches)
			fq_data.Pri = 100

			mb = mblist[i]
			if ep1 != nil {
				_, _, fq_data.Swid, _ = get_hostinfo( ep1 )		// if a specific src host supplied, get it's switch and we'll land only one flow-mod on it
			} else {
				fq_data.Swid = nil								// if ep1 is undefined (all), then we need a f-mod on all switches to handle ingress case
			}
			fq_data.Action.Tpsport = &zero_string				// no speicifc match here
			fq_data.Nxt_mac = mb.Get_mac( )
			jstr, _ := fq_data.To_json( ) 
			rm_sheep.Baa( 2, "write ingress fmod: %s", jstr )

			msg := ipc.Mk_chmsg()
			msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// no response right now -- eventually we want an asynch error
		} else {																// push fmod on the switch that connects the previous mbox matching packets from it and directing to next mbox

			mb = mblist[i-1] 													// pull previous middlebox which will be the source and define the swtich and port for this rule
			fq_data.Swid, fq_data.Match.Swport = mb.Get_sw_port( )	 			// specific switch and input port needed for this fmod
			fq_data.Lbmac = mb.Get_mac()										// fqmgr will need the mac if the port is late binding (-128)
			fq_data.Match.Smac = nil											// we match based on input port and dest mac, so no need for this
			fq_data.Match.Ip1 = nil												// and no need for the source ip which fqmanager happily translates to a mac

			fq_data.Pri = 200						// priority for intermediate flow-mods
			mb = mblist[i]	 						// now safe to get next middlebox in the list
			fq_data.Nxt_mac = mb.Get_mac( )
			jstr, _ := fq_data.To_json( ) 
			rm_sheep.Baa( 2, "write intemed fmod: %s", jstr )

			msg := ipc.Mk_chmsg()
			msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// flow mod for each intermediate link in backwards direction
		}
	}
}


/*
	Push the fmod requests to fq-mgr for a steering resrvation. 
*/
func push_st_reservation( p *gizmos.Pledge, rname string, ch chan *ipc.Chmsg ) {

	ep1, ep2, _, _, _, conclude, _, _ := p.Get_values( )		// hosts, ports and expiry are all we need
	now := time.Now().Unix()

	ep1 = name2ip( ep1 )										// we work only with IP addresses; sets to nil if "" (L*)
	ep2 = name2ip( ep2 )

	nmb := p.Get_mbox_count()
	mblist := make( []*gizmos.Mbox, nmb ) 
	for i := range mblist {
		mblist[i] = p.Get_mbox( i )
	}
	steer_fmods( ep1, ep2, mblist, conclude - now, &rname, p.Get_proto(), true )			// set forward fmods

	nmb--
	for i := range mblist {											// build middlebox list in reverse
		mblist[nmb-i] = p.Get_mbox( i )
	}
	steer_fmods( ep2, ep1, mblist, conclude - now, &rname, p.Get_proto(), false )			// set backward fmods

	p.Set_pushed()
}
