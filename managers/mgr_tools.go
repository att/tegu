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

	Mnemonic:	mgr_tools
	Abstract:	A collection of functions that are shared across all management types in the package.
					
	Date:		08 June 2015
	Author:		E. Scott Daniels

	Mods:
*/

package managers
import (
	"strings"
)


import (

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/ipc"
)


/*
	Send a request to openstack interface for a host list. We will _not_ wait on it
	and the caller is expected to handle the response written to the provided channel.
*/
func req_hosts(  rch chan *ipc.Chmsg, sheep *bleater.Bleater ) {
	sheep.Baa( 2, "requesting host list from osif" )

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, rch, REQ_CHOSTLIST, nil, nil )
}

/*
	Given a VM name of the form project/stuff, or just stuff, return stuff.
*/
func strip_project( name *string ) ( *string ) {

	if name == nil {
		return nil
	}

	toks := strings.SplitN( *name, "/", 2 )
	if len( toks ) < 2 {					// no project id in the name, assume  just IP
		return name
	}

	return &toks[1]
}

/*
	Gathers information about the host from openstack, and if known inserts the information into
	the network graph. If block is true, then we will block on a repl from network manager.
	If update_fqmgr is true, then we will also send osif a request to update the fqmgr with
	data that might ahve changed as a result of lazy gathering of info by the get_hostinfo
	request.  If block is set, then we block until osif acks the request. This ensures
	that the request has been given to fq-mgr which is single threaded and thus will process
	the update before attempting to process any flow-mods that result from a later reservation.
*/
func update_graph( hname *string, update_fqmgr bool, block bool ) {

	my_ch := make( chan *ipc.Chmsg )							// allocate channel for responses to our requests

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, my_ch, REQ_GET_HOSTINFO, hname, nil )				// request data
	req = <- my_ch
	if req.Response_data != nil {												// if returned send to network for insertion
		if ! block {
			my_ch = nil															// turn off if not blocking
		}

		req.Send_req( nw_ch, my_ch, REQ_ADD, req.Response_data, nil )			// add information to the graph
		if block {
			_ = <- my_ch															// wait for response -- at the moment we ignore
		}
	} else {
		if req.State != nil {
			http_sheep.Baa( 2, "unable to get host info for %s: %s", *hname, req.State )		// this is probably ok as it's likely a !//ipaddress hostname, but we'll log it anyway
		}
	}

	if update_fqmgr {
		req := ipc.Mk_chmsg( )
		req.Send_req( osif_ch, my_ch, REQ_IP2MACMAP, hname, nil )				// cause osif to push changes into fq-mgr (caution: we give osif fq-mgr's channel for response)
		if block {
			_ = <- my_ch
		}
	}
}

/*
	This function accepts a string of the form proj/epid/address or just the endpoint
	uuid (epid), and returns the address or the endpoint uuid, depending on the 
	setting of pull_ep.  If pull_ep is true, then the endpoint id is returned 
	otherwise the ip address is returned.  If the input string is just an endpoint
	id, then a message is sent to the network thread to pull the first (default) ip 
	address assocated with the endpoint (if pull_ep is false), otherwise the endpoint 
	id passed in is returned (caller doesn't need to know that it is or isn't a 
	pea string.   Confused???  Just use the name2ip() and name2ep() wrapper functions.
*/
func pull_from_pea_str( name *string, pull_ep bool ) ( s *string ) {
	s = nil

	if name == nil || *name == "" {
		return
	}

	tokens := strings.Split( *name, "/" )
	if len( tokens ) > 2 {
		dup := tokens[1]
		if !pull_ep {
			dup = tokens[2]
		}
		return &dup
	}

	if ! pull_ep {
		ch := make( chan *ipc.Chmsg )	
		defer close( ch )									// close it on return
		msg := ipc.Mk_chmsg( )
		msg.Send_req( nw_ch, ch, REQ_GETIP, name, nil )
		msg = <- ch
		if msg.State == nil {					// success
			s = msg.Response_data.( *string )
		}
	}

	return s
}

/*
	Given a pea string or endpoint id string return the associated IP address.
	For an endpoint string this will be the "default" ip and may not be what 
	is expected.
*/
func name2ip( pea *string ) ( *string ) {
	return pull_from_pea_str( pea, false )
}

/*	
	Given a pea string or endpoint id string return the associated endpoint id.
*/
func name2ep( pea *string )  ( *string ) {
	return pull_from_pea_str( pea, true )
}


/*
	Given an endpoint uuid get network manager to xlate that to a mac address.
*/
func epid2mac( epid *string ) ( string ) {
	var ok bool

	rch := make( chan *ipc.Chmsg )
	req := ipc.Mk_chmsg( )
	req.Send_req( nw_ch, rch, REQ_EP2MAC, *epid, nil )
	req = <-rch
	mac := ""
	if mac, ok = req.Response_data.( string ); ok {
		return mac
	}

	return ""
}
