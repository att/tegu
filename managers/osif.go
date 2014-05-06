// vi: sw=4 ts=4:

/*

	Mnemonic:	osif -- openstack interface manager
	Abstract:	Manages the interface to all defined openstack environments.
				The main function here should be executed as a goroutine and will 
				set up various ticklers to drive things like the rebuilding of
				the vm2ip map. Other components may send requests to the osif_mgr
				as needed. 

				Maps built:
					openstack, VMs (ID and name) to IP address

				The trick with openstack is that there may be more than one project
				(tenant) that we need to find VMs in.  We will depend on the config
				file data (global) which should contain a list of each openstack section defined in the config, and for each section we expect it
				to contain:

					url 	the url for the authorisation e.g. "http://135.197.225.209:5000/"
    				usr 	the user name that can be authorised with passwd
    				passwd	the password 
    				project	the project/tenant name

				For each section an openstack object is created and for each openstack
				related translation that is needed all objects will be used to query
				openstack.   At present there is no means to deal with information 
				that might be duplicated between openstack projects. (This might become
				an issue if user defined networking selects the same address range(s).

	Date:		28 December 2013
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Changes to support digging the various maps out of openstack
					that are needed when we are not using floodlight.
*/

package managers

import (
	//"bufio"
	//"errors"
	"fmt"
	//"io"
	"os"
	"strings"
	//"time"

	"forge.research.att.com/gopkgs/bleater"
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/gopkgs/ostack"
	"forge.research.att.com/gopkgs/token"
	//"forge.research.att.com/tegu/gizmos"
)

//var (
// NO GLOBALS HERE; use globals.go
//)


// --- Private --------------------------------------------------------------------------

func mapvm2ip( os_refs []*ostack.Ostack ) ( m  map[string]*string ) {
	var (
		err	error
	)
	
	m = nil
	for i := 0; i < len( os_refs ); i++ {
		osif_sheep.Baa( 1, "mapping VMs from: %s", os_refs[i].To_str( ) )
		m, err = os_refs[i].Mk_vm2ip( m )
		if err != nil {
			osif_sheep.Baa( 1, "WRN: mapvm2ip: openstack query failed: %s", err )
		}
	}
	
	return
}

/*
	returns a list of openstack compute hosts
*/
func get_hosts( os_refs []*ostack.Ostack ) ( s *string, err error ) {
	var (
		ts 		string = ""
		list	*string			// list of host from the openstack world
	)

	s = nil
	err = nil
	sep := ""

	if os_refs == nil || len( os_refs ) <= 0 {
		err = fmt.Errorf( "no openstack hosts in list to query" )
		return
	}

	for i := 0; i < len( os_refs ); i++ {
		list, err = os_refs[i].List_hosts( ostack.COMPUTE )	
		if err != nil {
			osif_sheep.Baa( 0, "WRN: error accessing host list: for %s: %s", os_refs[i].To_str(), err )
			return							// drop out on first error with no list
		} else {
			if *list != "" {
				ts += sep + *list
				sep = " "
			} else {
				osif_sheep.Baa( 1, "WRN: list of hosts not returned by %s", os_refs[i].To_str() )	
			}
		}
	}

	cmap := token.Tokenise_count( ts, " " )		// break the string, and then dedup the values
	ts = ""
	sep = ""
	for k, v := range( cmap ) {
		if v > 0 { 
			ts += sep + k
			sep = " "
		}
	}

	s = &ts
	return
}

/*
	Tegu-lite
	Build all vm translation maps -- requires two actual calls out to openstack
*/
func map_all( os_refs []*ostack.Ostack, inc_tenant bool  ) ( 
			vmid2ip map[string]*string, 
			ip2vmid map[string]*string, 
			vm2ip map[string]*string, 
			vmid2host map[string]*string, 
			ip2mac map[string]*string, 
			gwmap map[string]*string,
			rerr error ) {
	
	var (
		err error
	)

	vmid2ip = nil				// shouldn't need, but safety never hurts
	ip2vmid = nil
	vm2ip = nil
	vmid2host = nil
	ip2mac = nil
	gwmap = nil				// mac2ip for all gateway "boxes"

	for i := 0; i < len( os_refs ); i++ {
		osif_sheep.Baa( 1, "creating VM maps from: %s", os_refs[i].To_str( ) )
		vmid2ip, ip2vmid, vm2ip, vmid2host, err = os_refs[i].Mk_vm_maps( vmid2ip, ip2vmid, vm2ip, vmid2host, inc_tenant )
		if err != nil {
			osif_sheep.Baa( 1, "WRN: unable to map VM info: %s; %s", os_refs[i].To_str( ), err )
			rerr = err
		}
	}

	ip2mac, _, err = os_refs[0].Mk_mac_maps( nil, nil, inc_tenant )	// only need to get once -- ostack sends back all network things in one batch
	if err != nil {
		osif_sheep.Baa( 1, "WRN: unable to map MAC info: %s; %s", os_refs[0].To_str( ), err )
		rerr = err
	}

	
	gwmap, _, err = os_refs[0].Mk_gwmaps( inc_tenant )

	return
}

func get_ip2mac( os_refs []*ostack.Ostack, inc_tenant bool ) ( m map[string]*string, err error ) {
	m, _, err = os_refs[0].Mk_mac_maps( nil, nil, inc_tenant )	
	if err != nil {
		osif_sheep.Baa( 1, "WRN: unable to map MAC info: %s; %s", os_refs[0].To_str( ), err )
	}

	return
}


// --- Public ---------------------------------------------------------------------------


/*
	executed as a goroutine this loops waiting for messages from the tickler and takes 
	action based on what is needed. 
*/
func Osif_mgr( my_chan chan *ipc.Chmsg ) {

	var (
		msg	*ipc.Chmsg
		os_list string = ""
		os_sects	[]string
		os_refs		[]*ostack.Ostack			// reference to each openstack project we need to query
		inc_tenant	bool = false
		refresh_delay	= 180					// config file can override
	)

	osif_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	osif_sheep.Set_prefix( "osif_mgr" )
	tegu_sheep.Add_child( osif_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	// ---- pick up configuration file things of interest --------------------------
	if p := cfg_data["osif"]["include_tenant"]; p != nil {
		if *p == "true" {
			inc_tenant = true
		}
	} 

	if p := cfg_data["osif"]["refresh"]; p != nil {
		refresh_delay = clike.Atoi( *p ); 			
		if refresh_delay < 30 {
			osif_sheep.Baa( 1, "WRN osif:resresh was too small (%ds), setting to 30s", refresh_delay )
			refresh_delay = 30
		}
	} 

	p := cfg_data["osif"]["ostack_list"] 				// preferred placement in osif section
	if p == nil {
		p = cfg_data["default"]["ostack_list"] 			// originally in default, so backwards compatable
	}
	if p != nil {
		os_list = *p
	} 

	if os_list == " " || os_list == "" || os_list == "off" {
		osif_sheep.Baa( 0, "WRN: osif disabled: no openstack list (ostack_list) defined in configuration file or setting is 'off'" )
	} else {

		if strings.Index( os_list, "," ) > 0 {
			os_sects = strings.Split( os_list, "," )
		} else {
			os_sects = strings.Split( os_list, " " )
		}
	
		os_refs = make( []*ostack.Ostack, len( os_sects ) )
		for i := 0; i < len( os_sects ); i++ {
			osif_sheep.Baa( 1, "creating openstack interface for %s", os_sects[i] )
			os_refs[i] = ostack.Mk_ostack( cfg_data[os_sects[i]]["url"], cfg_data[os_sects[i]]["usr"], cfg_data[os_sects[i]]["passwd"], cfg_data[os_sects[i]]["project"] )
		}
	}
	// ---------------- end config parsing ----------------------------------------


	tklr.Add_spot( 3, my_chan, REQ_GENMAPS, nil, 1 )						// add tickle spot to drive us once in 3s and then another to drive us every 180s 
	tklr.Add_spot( 180, my_chan, REQ_GENMAPS, nil, ipc.FOREVER );  	

	osif_sheep.Baa( 2, "osif manager is running  %x", my_chan )
	for ;; {
		msg = <- my_chan					// wait for next message from tickler
		msg.State = nil						// default to all OK

		osif_sheep.Baa( 3, "processing request: %d", msg.Msg_type )
		switch msg.Msg_type {
			case REQ_GENMAPS:							// driven by tickler; gen a new set of VM translation maps and pass them to network manager
				vmid2ip, ip2vmid, vm2ip, vmid2host, ip2mac, gwmap, err := map_all( os_refs, inc_tenant )
				if err == nil {
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VM2IP, vm2ip, nil )					// send w/o expecting anything back
	
					msg = ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VMID2IP, vmid2ip, nil )					
	
					msg = ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_IP2VMID, ip2vmid, nil )				
	
					msg = ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VMID2PHOST, vmid2host, nil )		
	
					msg = ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_IP2MAC, ip2mac, nil )		
	
					msg = ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_GWMAP, gwmap, nil )		
	
					osif_sheep.Baa( 1, "VM maps were updated from openstack" )
				} else {
					osif_sheep.Baa( 0, "ERR: fetching VM maps failed: %s", err )
				}

	/* ---- before lite ----
			case REQ_VM2IP:														// driven by tickler; gen a new vm translation map and push to net mgr
				m := mapvm2ip( os_refs )
				if m != nil {
					count := 0;
					msg := ipc.Mk_chmsg( )
					msg.Send_req( nw_ch, nil, REQ_VM2IP, m, nil )					// send new map to network as it is managed there
					osif_sheep.Baa( 1, "VM2IP mapping updated from openstack" )
					for k, v := range m {
						osif_sheep.Baa( 2, "VM mapped: %s ==> %s", k, *v )
						count++;
					}
					osif_sheep.Baa( 2, "mapped %d VM names/IDs from openstack", count )
				}
	*/

			case REQ_IP2MACMAP:													// generate an ip to mac map for the caller and write on the channel
				if msg.Response_ch != nil {										// no sense going off to ostack if no place to send the list
					msg.Response_data, msg.State = get_ip2mac( os_refs, inc_tenant )
				} else {
					osif_sheep.Baa( 0, "WRN: no response channel for host list request" )
				}

			case REQ_CHOSTLIST:
				if msg.Response_ch != nil {										// no sense going off to ostack if no place to send the list
					msg.Response_data, msg.State = get_hosts( os_refs )
				} else {
					osif_sheep.Baa( 0, "WRN: no response channel for host list request" )
				}

			default:
				osif_sheep.Baa( 1, "unknown request: %d", msg.Msg_type )
				msg.Response_data = nil
				if msg.Response_ch != nil {
					msg.State = fmt.Errorf( "osif: unknown request (%d)", msg.Msg_type )
				} 
		}

		osif_sheep.Baa( 3, "processing request complete: %d", msg.Msg_type )
		if msg.Response_ch != nil {			// if a reqponse channel was provided
			msg.Response_ch <- msg			// send our result back to the requestor
		}
	}
}
