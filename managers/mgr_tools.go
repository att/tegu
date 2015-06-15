// vi: sw=4 ts=4:

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

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/ipc"
)


/*
	Send a request to openstack interface for a host list. We will _not_ wait on it 
	and will handle the response in the main loop. 
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
