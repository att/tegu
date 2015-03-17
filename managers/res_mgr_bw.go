// vi: sw=4 ts=4:

/*

	Mnemonic:	res_mgr_bw
	Abstract:	reservation manager functions that are directly related to bandwidth
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		
*/

package managers

import (
	"strings"
	"time"

	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)

/*
	For a single pledge this function builds the necessary flow-mod directives and gives them to 
	the fq_manager to dispatch down to the agents.

	With the new method of managing queues per reservation on ingress/egress hosts, we now send to fq_mgr:
		h1, h2 -- hosts
		expiry
		switch/port/queue
	
	for each 'link' in the forward direction, and then we reverse the path and send requests to fq_mgr
	for each 'link' in the backwards direction.  Errors are returned to res_mgr via channel, but
	asycnh; we do not wait for responses to each message generated here. If set_vlan is true then
	we will send the src mac address on the flow-mod at ingress so that the vlan is properly set
	(suports br-rl in a GRE environment).

	To_limit is a cap to the expiration time sent when creating a flow-mod.  OVS (and others we assume)
	use an unsigned int32 as a hard timeout value, and thus have an upper limit of just over 18 hours. If
	to_limit is > 0, we'll ensure that the timeout passed on the request to fq-mgr won't exceed  the limit,
 	and we assume that this function is called periodically to update long running reservations.
*/
func push_bw_reservations( p *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, set_vlan bool, to_limit int64 ) {
	var (
		msg		*ipc.Chmsg
		ip2		*string					// the ip ad
	)

	now := time.Now().Unix()

	h1, h2, p1, p2, _, expiry, _, _ := p.Get_values( )		// hosts, ports and expiry are all we need

	ip1 := name2ip( h1 )
	ip2 = name2ip( h2 )

	if ip1 != nil  &&  ip2 != nil {				// good ip addresses so we're good to go
		plist := p.Get_path_list( )				// each path that is a part of the reservation

		timestamp := time.Now().Unix() + 16					// assume this will fall within the first few seconds of the reservation as we use it to find queue in timeslice

		for i := range plist { 								// for each path, send fmod requests for each endpoint and each intermed link, both forwards and backwards
			fmod := Mk_fqreq( rname )						// default flow mod request with empty match/actions

			if *p1 != "0" || *p2 != "0" {					// port oriented flow-mods get a slightly higher priority
				fmod.Pri =	405								// override the defaults
			} else {
				fmod.Pri =	400								// no port specification, higher than most, but allow a match on port first
			}
			fmod.Cookie =	0xdead
			fmod.Single_switch = false						// path involves multiple switches by default
			fmod.Dscp, fmod.Dscp_koe = p.Get_dscp()			// reservation supplied dscp value that we're to match and maybe preserve on exit

			if p.Is_paused( ) {
				fmod.Expiry = time.Now().Unix( ) +  15		// if reservation shows paused, then we set the expiration to 15s from now  which should force the flow-mods out
			} else {
				if to_limit > 0 && expiry > now + to_limit {
					fmod.Expiry = now + to_limit			// expiry must be capped so as not to overflow virtual switch variable size
				} else {
					fmod.Expiry = expiry
				}
			}
			fmod.Id = rname

			nlinks := plist[i].Get_nlinks() 				// if only one link, then we DONT set vlan later
			extip := plist[i].Get_extip()					// if an external IP address is necessary on the fmod get it
			if extip != nil {
				fmod.Extip = extip
			} else {
				fmod.Extip = &empty_str
			}

			espq1, _ := plist[i].Get_endpoint_spq( rname, timestamp )		// endpoints are saved h1,h2, but we need to process them in reverse here

											//FUTURE: accept proto=udp or proto=tcp on the reservation to provide ability to limit, or supply alternate protocols
			tptype_list := "none"							// default to no specific protocol 
			if *p1 != "0" || *p2 != "0" {					// if either port is specified, then we need to generate for both udp and tcp
				tptype_list = "udp tcp"						// if port supplied, generate f-mods for both udp and tcp matches on the port
			}
			tptype_toks := strings.Split( tptype_list, " " )

			for tidx := range( tptype_toks ) {				// must have a flow mod for each transport protocol type
				fmod.Tptype = &tptype_toks[tidx]

				fmod.Match.Tpsport= p1											// forward direction transport ports are h1==src h2==dest
				fmod.Match.Tpdport= p2
				fmod.Match.Ip1, _ = plist[i].Get_h1().Get_addresses()			// forward first, from h1 -> h2 (must use info from path as it might be split)
				fmod.Match.Ip2, _ = plist[i].Get_h2().Get_addresses()
				fmod.Exttyp = plist[i].Get_extflag()

				rm_sheep.Baa( 1, "res_mgr/push_reg: sending i/e flow-mods for path %d: %s flag=%s tptyp=%s h1=%s --> h2=%s ip1= %s ip2=%s ext=%s exp/fm_exp=%d/%d",
					i, *rname, *fmod.Exttyp, tptype_toks[tidx], *h1, *h2, *fmod.Match.Ip1, *fmod.Match.Ip2, *fmod.Extip, expiry, fmod.Expiry )


				// ---- push flow-mods in the h1->h2 direction -----------
				if espq1 != nil {													// data flowing into h2 from h1 over h2 to switch connection (ep0 handled with reverse path)
																					// ep will be nil if both VMs are on the same switch
					cfmod := fmod.Clone( )											// must send a copy since we put multiple flowmods onto the fq-mgr queue
					cfmod.Dir_in = true
					cfmod.Espq = espq1
					msg = ipc.Mk_chmsg()
					msg.Send_req( fq_ch, ch, REQ_IE_RESERVE, cfmod, nil )			// queue work to send to skoogi (errors come back asynch, successes do not generate response)
				} else {
					fmod.Single_switch = true
				}

				cfmod := fmod.Clone( )
				cfmod.Espq = plist[i].Get_ilink_spq( rname, timestamp )				// send fmod to ingress switch on first link out from h1
				cfmod.Dir_in = false
				if nlinks > 1 && set_vlan {
					cfmod.Action.Vlan_id = cfmod.Match.Ip1								// use mac address -- agent will convert to the vlan-id assigned to it
				}
				msg = ipc.Mk_chmsg()
				msg.Send_req( fq_ch, ch, REQ_IE_RESERVE, cfmod, nil )					// queue work to send to skoogi (errors come back asynch, successes do not generate response)

				ilist := plist[i].Get_forward_im_spq( timestamp )						// get list of intermediate switch/port/qnum data in forward (h1->h2) direction
				for ii := range ilist {
					cfmod = fmod.Clone( )												// copy to pass which we'll alter a wee bit
					cfmod.Espq = ilist[ii]
					rm_sheep.Baa( 2, "send forward intermediate reserve: [%d] %s %d %d", ii, ilist[ii].Switch, ilist[ii].Port, ilist[ii].Queuenum )
					msg = ipc.Mk_chmsg()
					msg.Send_req( fq_ch, ch, REQ_IE_RESERVE, cfmod, nil )			// flow mod for each intermediate link in foward direction
				}
			}
		}

		p.Set_pushed()				// safe to mark the pledge as having been pushed.
	}
}
