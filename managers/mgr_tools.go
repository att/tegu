// vi: sw=4 ts=4:

/*

	Mnemonic:	mgr_tools
	Abstract:	A collection of functions that are shared across all management threads in the package.
					
	Date:		08 June 2015
	Author:		E. Scott Daniels

	Mods:
*/

package managers

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
