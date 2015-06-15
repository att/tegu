// vi: sw=4 ts=4:

/*

	Mnemonic:	res_mgr_bw
	Abstract:	reservation manager functions that are directly related to bandwidth
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		
				26 May 2015 - Changes to support pledge as an interface.
				11 Jun 2015 - Added bwow support and renamed bw push function.
*/

package managers

import (
	"strings"
	"time"

	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)

/*
	For a single bandwidth pledge, this function sets things up and sends needed requests to the fq-manger to 
	create any necessary flow-mods.   This has changed drastically now that we expect one agent 
	onvocation to set up all bandwidth flow-mods for an endpoint switch.

	With the new method of managing queues per reservation on ingress/egress hosts, we now send 
	the following information to fq_mgr:
		h1, h2 -- hosts
		expiry
		switch/port/queue
	
	Path list will have path(s) in both directions (to support different bandwidth rates).
	The above info is sent for each 'link' in the forward direction path, and then we reverse 
	the path and send requests to fq_mgr for each 'link' in the backwards direction.  Errors are 
	returned to res_mgr via channel, but asycnh; we do not wait for responses to each message 
	generated here. 

	To_limit is a cap to the expiration time sent when creating a flow-mod.  OVS (and others we assume)
	use an unsigned int32 as a hard timeout value, and thus have an upper limit of just over 18 hours. If
	to_limit is > 0, we'll ensure that the timeout passed on the request to fq-mgr won't exceed  the limit,
 	and we assume that this function is called periodically to update long running reservations.

	Alt_table is the base alternate table set that we use for meta marking

	If pref_ip6 is true, then if a host has both v4 and v6 addresses we will use the v6 address.
*/
func bw_push_res( gp *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, to_limit int64, alt_table int, pref_v6 bool ) {
	var (
		msg		*ipc.Chmsg
	)

	now := time.Now().Unix()

	p, ok :=  (*gp).( *gizmos.Pledge_bw )		// generic pledge better be a bw pledge!
	if ! ok {
		rm_sheep.Baa( 1, "internal error in push_bw_reservation: pledge isn't a bandwidth pledge" )
		(*gp).Set_pushed()						// prevent looping
		return
	}

	h1, h2, p1, p2, _, expiry, _, _ := p.Get_values( )		// hosts, transport (tcp/udp) ports and expiry are all we need
	v1, v2 := p.Get_vlan( )									// vlan match criteria for one/both endpoints

	ip1 := name2ip( h1 )
	ip2 := name2ip( h2 )

	if ip1 != nil  &&  ip2 != nil {				// good ip addresses so we're good to go
		plist := p.Get_path_list( )				// each path that is a part of the reservation

		timestamp := time.Now().Unix() + 16					// assume this will fall within the first few seconds of the reservation as we use it to find queue in timeslice

		for i := range plist { 								// for each path, send fmgr requests for each endpoint
			freq := Mk_fqreq( rname )						// default flow mod request with empty match/actions (for bw requests, we don't need priority or such things)

			freq.Ipv6 = p.Get_matchv6()						// should we force a match on IPv6 rather than IPv4?
			freq.Cookie =	0xffff							// should be ignored, if we see this out there we've got problems
			freq.Single_switch = false						// path involves multiple switches by default
			freq.Dscp, freq.Dscp_koe = p.Get_dscp()			// reservation supplied dscp value that we're to match and maybe preserve on exit

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

			extip := plist[i].Get_extip()					// if an external IP address is necessary on the freq get it
			if extip != nil {
				freq.Extip = extip
			} else {
				freq.Extip = &empty_str
			}

			espq1, _ := plist[i].Get_endpoint_spq( rname, timestamp )		// end point switch, port, queue information; ep1 nil if single switch
			if espq1 == nil {												// if single switch ep1 will be nil
				freq.Single_switch = true
			}

			freq.Match.Ip1 = plist[i].Get_h1().Get_address( pref_v6 )		// must use path h1/h2 as this could be the reverse with respect to the overall pledge and thus reverse of pledge
			freq.Match.Ip2 = plist[i].Get_h2().Get_address( pref_v6 )
			freq.Espq = plist[i].Get_ilink_spq( rname, timestamp )			// spq info comes from the first link off of the switch, not the endpoint link back to the VM
			if freq.Single_switch {
				freq.Espq.Queuenum = 1										// same switch always over br-rl queue 1
			}
			freq.Exttyp = plist[i].Get_extflag()		// indicates whether the external IP is the source or dest along this path

											//FUTURE: accept proto=udp or proto=tcp on the reservation to provide ability to limit, or supply alternate protocols
			tptype_list := "none"							// default to no specific protocol 
			if *p1 != "0" || *p2 != "0" {					// if either port is specified, then we need to generate for both udp and tcp
				tptype_list = "udp tcp"						// if port supplied, generate f-mods for both udp and tcp matches on the port
			}
			tptype_toks := strings.Split( tptype_list, " " )

			for tidx := range( tptype_toks ) {				// must have a req for each transport proto type, clone base, add the proto specific changes, & send to fqmgr
				cfreq := freq.Clone()						// since we send this off for asynch processing we must make a copy

				cfreq.Tptype = &tptype_toks[tidx]			// transport type (tcp, udp or none)

				if *cfreq.Exttyp == "-S" {					// indicates that this is a 'reverse' path (h2 sending) and we must invert the Tp port numbers and vland ids
					cfreq.Match.Tpsport= p2
					cfreq.Match.Tpdport= p1
					cfreq.Match.Vlan_id= v2
				} else {
					cfreq.Match.Tpsport= p1
					cfreq.Match.Tpdport= p2
					cfreq.Match.Vlan_id= v1
				}

				rm_sheep.Baa( 1, "res_mgr/push_rea: forward endpoint flow-mods for path %d: %s flag=%s tptyp=%s VMs=%s,%s dir=%s->%s tpsport=%s  tpdport=%s  spq=%s/%d/%d ext=%s exp/fm_exp=%d/%d",
					i, *rname, *cfreq.Exttyp, tptype_toks[tidx], *h1, *h2, *cfreq.Match.Ip1, *cfreq.Match.Ip2, *cfreq.Match.Tpsport, *cfreq.Match.Tpdport, 
					cfreq.Espq.Switch, cfreq.Espq.Port, cfreq.Espq.Queuenum, *cfreq.Extip, expiry, cfreq.Expiry )

				msg = ipc.Mk_chmsg()
				msg.Send_req( fq_ch, ch, REQ_BW_RESERVE, cfreq, nil )					// queue work with fq-manger to send cmds for bandwidth f-mod setup
				
	
				// WARNING:  this is q-lite only -- there is no attempt to set up intermediate switches!
			}
		}

		p.Set_pushed()				// safe to mark the pledge as having been pushed.  
	}
}


/*
	This builds a fq-mgr request and passes it to the fq-mgr to 'refine' and send along 
	to the agent-manager for ultimate execution.  It might be possible to pass it 
	directly to the agent manager, but because res-mgr thinks in IP addresses, and 
	fq-manager (that might be the only source of IP->Mac translation which flow-mods
	need) is still left in the middle. 

	CAUTION: this function is called for both new reservations, _and_ to refresh
		reservations with expiry value further in the future than the switch can
		handle.  It is also invoked when pausing reservations and will cause new
		flow-mods to be sent with a short duration timeout to flush existing flow-mods
		from the switch.
*/
func bwow_push_res( gp *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, to_limit int64, pref_v6 bool ) {
	var (
		msg		*ipc.Chmsg
	)

	now := time.Now().Unix()
	p, ok :=  (*gp).( *gizmos.Pledge_bwow )		// generic pledge better be a bw oneway pledge!
	if ! ok {
		rm_sheep.Baa( 1, "internal mishap in push_bwow_res: pledge isn't a oneway pledge" )
		(*gp).Set_pushed()						// prevent looping
		return
	}

	src, dest, src_tpport, dest_tpport, _, expiry  := p.Get_values( )		// hosts, transport ports, and expiry time
	vlan := p.Get_vlan( )													// vlan match criteria for source
rm_sheep.Baa( 1, ">>>> vlan=%s", vlan )

	ip_src := name2ip( src )
	ip_dest := name2ip( dest )

	if ip_src != nil  &&  ip_dest != nil {				// good ip addresses so we're good to go
		gate := p.Get_gate( )							// get the gate information that is applied for the oneway
		if gate != nil {								// be parinoid
			//timestamp := time.Now().Unix() + 16				// assume this will fall within the first few seconds of the reservation as we use it to find queue in timeslice
			freq := Mk_fqreq( rname )						// default flow mod request no match/actions

			freq.Ipv6 = p.Get_matchv6()						// should we force a match on IPv6 rather than IPv4?
			freq.Cookie =	0xffff							// should be ignored, if we see this out there we've got problems
			freq.Single_switch = true						// implied with a oneway, but set it anyway
			freq.Dscp = p.Get_dscp()						// reservation supplied dscp value that we're to match (koe is meaningless in one way)
			freq.Dscp_koe = false							// meaningless for oneway, but ensure it's false so flag isn't accidently set later

			if (*p).Is_paused( ) {
				freq.Expiry = time.Now().Unix( ) +  15		// if reservation shows paused, then we set the expiration to 15s from now  which should force existing flow-mods out
			} else {
				if to_limit > 0 && expiry > now + to_limit {
					freq.Expiry = now + to_limit			// expiry must be capped so as not to overflow virtual switch variable size
				} else {
					freq.Expiry = expiry
				}
			}
			freq.Id = rname

			freq.Match.Ip1 = gate.Get_src().Get_address( pref_v6 )		// should match pledge, but gate is the ultimate authority
			freq.Match.Ip2 = gate.Get_dest().Get_address( pref_v6 )
			freq.Espq = gate.Get_spq( )									// switch port queue
			freq.Extip = gate.Get_extip( )								// returns nil if not an external and that's what we need


											//FUTURE: accept proto=udp or proto=tcp on the reservation to provide ability to limit, or supply alternate protocols
			tptype_list := "none"							// default to no specific protocol 
			if *src_tpport != "0" || *dest_tpport != "0" {	// if either port is specified, then we need to generate for both udp and tcp
				tptype_list = "udp tcp"						// if port supplied, generate f-mods for both udp and tcp matches on the port
			}
			tptype_toks := strings.Split( tptype_list, " " )

			for tidx := range( tptype_toks ) {				// must have a req for each transport proto type, clone base, add the proto specific changes, & send to fqmgr
				cfreq := freq.Clone()						// since we send this off for asynch processing we must make a copy

				cfreq.Tptype = &tptype_toks[tidx]			// transport type (tcp, udp or none)

				cfreq.Match.Tpsport= src_tpport
				cfreq.Match.Tpdport= dest_tpport
				cfreq.Match.Vlan_id= vlan

				ip2_str := ""
				if cfreq.Match.Ip2 != nil {
					ip2_str = *cfreq.Match.Ip2
				}
				rm_sheep.Baa( 1, "res_mgr/push_bwow: flag=%s tptyp=%s VMs=%s,%s dir=%s->%s tpsport=%s  tpdport=%s  spq=%s/%d/%d exp/fm_exp=%d/%d",
					*rname, tptype_toks[tidx], *src, *dest, *cfreq.Match.Ip1, ip2_str, *cfreq.Match.Tpsport, *cfreq.Match.Tpdport, 
					cfreq.Espq.Switch, cfreq.Espq.Port, cfreq.Espq.Queuenum, expiry, cfreq.Expiry )

				msg = ipc.Mk_chmsg()
				msg.Send_req( fq_ch, ch, REQ_BWOW_RESERVE, cfreq, nil )					// queue work with fq-manger to send cmds for bandwidth f-mod setup
				
			}
		}

		p.Set_pushed()				// safe to mark the pledge as having been pushed.  
	} else {
		rm_sheep.Baa( 1, "oneway not pushed: could not map one/both hosts to an IP address" )
	}
}
