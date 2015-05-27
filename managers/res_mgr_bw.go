// vi: sw=4 ts=4:

/*

	Mnemonic:	res_mgr_bw
	Abstract:	reservation manager functions that are directly related to bandwidth
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		
				26 May 2015 - Changes to support pledge as an interface.
*/

package managers

import (
	"strings"
	"time"

	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)

/*
	For a single pledge this function sets things up and sends needed requests to the fq-manger to 
	create any necessary flow-mods.   This has changed drastically now that we expect one agent 
	onvocation to set up all bandwidth flow-mods for an endpoint switch.

	With the new method of managing queues per reservation on ingress/egress hosts, we now send to fq_mgr:
		h1, h2 -- hosts
		expiry
		switch/port/queue
	
	for each 'link' in the forward direction, and then we reverse the path and send requests to fq_mgr
	for each 'link' in the backwards direction.  Errors are returned to res_mgr via channel, but
	asycnh; we do not wait for responses to each message generated here. 

	To_limit is a cap to the expiration time sent when creating a flow-mod.  OVS (and others we assume)
	use an unsigned int32 as a hard timeout value, and thus have an upper limit of just over 18 hours. If
	to_limit is > 0, we'll ensure that the timeout passed on the request to fq-mgr won't exceed  the limit,
 	and we assume that this function is called periodically to update long running reservations.

	Alt_table is the base alternate table set that we use for meta marking

	If pref_ip6 is true, then if a host has both v4 and v6 addresses we will use the v6 address.
*/
//func push_bw_reservations( p *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, set_vlan bool, to_limit int64, alt_table int, pref_ip6 bool ) {

func push_bw_reservations( gp *gizmos.Pledge, rname *string, ch chan *ipc.Chmsg, to_limit int64, alt_table int, pref_v6 bool ) {
	var (
		msg		*ipc.Chmsg
		ip2		*string					// the ip ad
	)

	now := time.Now().Unix()

	p, ok :=  (*gp).( *gizmos.Pledge_bw )		// generic pledge better be a bw pledge!
	if ! ok {
		rm_sheep.Baa( 1, "internal error in push_bw_reservation: pledge isn't a bandwidth pledge" )
		(*gp).Set_pushed()						// prevent looping
		return
	}

	h1, h2, p1, p2, _, expiry, _, _ := p.Get_values( )		// hosts, ports and expiry are all we need

	ip1 := name2ip( h1 )
	ip2 = name2ip( h2 )

	if ip1 != nil  &&  ip2 != nil {				// good ip addresses so we're good to go
		plist := p.Get_path_list( )				// each path that is a part of the reservation

		timestamp := time.Now().Unix() + 16					// assume this will fall within the first few seconds of the reservation as we use it to find queue in timeslice

		for i := range plist { 								// for each path, send fmod requests for each endpoint and each intermed link, both forwards and backwards
			fmod := Mk_fqreq( rname )						// default flow mod request with empty match/actions (for bw requests, we don't need priority or such things)

			fmod.Ipv6 = (*p).Get_matchv6()					// should we force a match on IPv6 rather than IPv4?
			fmod.Cookie =	0xffff							// should be ignored, if we see this out there we've got problems
			fmod.Single_switch = false						// path involves multiple switches by default
			fmod.Dscp, fmod.Dscp_koe = p.Get_dscp()		// reservation supplied dscp value that we're to match and maybe preserve on exit

			if (*p).Is_paused( ) {
				fmod.Expiry = time.Now().Unix( ) +  15		// if reservation shows paused, then we set the expiration to 15s from now  which should force the flow-mods out
			} else {
				if to_limit > 0 && expiry > now + to_limit {
					fmod.Expiry = now + to_limit			// expiry must be capped so as not to overflow virtual switch variable size
				} else {
					fmod.Expiry = expiry
				}
			}
			fmod.Id = rname

			extip := plist[i].Get_extip()					// if an external IP address is necessary on the fmod get it
			if extip != nil {
				fmod.Extip = extip
			} else {
				fmod.Extip = &empty_str
			}

			espq1, _ := plist[i].Get_endpoint_spq( rname, timestamp )		// end point switch, port, queue information; ep1 nil if single switch
			if espq1 == nil {													// if single switch ep1 will be nil; if it's here we need to send fmods to that side too
				fmod.Single_switch = true
			}

											//FUTURE: accept proto=udp or proto=tcp on the reservation to provide ability to limit, or supply alternate protocols
			tptype_list := "none"							// default to no specific protocol 
			if *p1 != "0" || *p2 != "0" {					// if either port is specified, then we need to generate for both udp and tcp
				tptype_list = "udp tcp"						// if port supplied, generate f-mods for both udp and tcp matches on the port
			}
			tptype_toks := strings.Split( tptype_list, " " )

			for tidx := range( tptype_toks ) {				// must have a flow-mod set for each transport protocol type
				cfmod := fmod.Clone()						// since we send this off for asynch processing we must make a copy

				cfmod.Tptype = &tptype_toks[tidx]
				cfmod.Exttyp = plist[i].Get_extflag()

				if *cfmod.Exttyp == "-S" {					// indicates that this is a 'reverse' path and we must invert the Tp port numbers
					cfmod.Match.Tpsport= p2
					cfmod.Match.Tpdport= p1
				} else {
					cfmod.Match.Tpsport= p1
					cfmod.Match.Tpdport= p2
				}
				cfmod.Match.Ip1 = plist[i].Get_h1().Get_address( pref_v6 )		// must use path h1/h2 as this could be the reverse with respect to the overall pledge and thus reverse of pledge
				cfmod.Match.Ip2 = plist[i].Get_h2().Get_address( pref_v6 )
				cfmod.Espq = plist[i].Get_ilink_spq( rname, timestamp )			// spq info comes from the first link off of the switch, not the endpoint link back to the VM
				if fmod.Single_switch {
					cfmod.Espq.Queuenum = 1										// same switch always over br-rl queue 1
				}

				rm_sheep.Baa( 1, "res_mgr/push_reg: forward endpoint flow-mods for path %d: %s flag=%s tptyp=%s VMs=%s,%s dir=%s->%s tpsport=%s  tpdport=%s  spq=%s/%d/%d ext=%s exp/fm_exp=%d/%d",
					i, *rname, *cfmod.Exttyp, tptype_toks[tidx], *h1, *h2, *cfmod.Match.Ip1, *cfmod.Match.Ip2, *cfmod.Match.Tpsport, *cfmod.Match.Tpdport, 
					cfmod.Espq.Switch, cfmod.Espq.Port, cfmod.Espq.Queuenum, *cfmod.Extip, expiry, cfmod.Expiry )

				msg = ipc.Mk_chmsg()
				msg.Send_req( fq_ch, ch, REQ_BW_RESERVE, cfmod, nil )					// queue work with fq-manger to send cmds for bandwidth f-mod setup
				
	
				// WARNING:  this is q-lite only -- there is no attempt to set up fmods on intermediate switches!
			}
		}

		p.Set_pushed()				// safe to mark the pledge as having been pushed.  
	}
}

