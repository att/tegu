// vi: sw=4 ts=4:

/*

	Mnemonic:	agent_wa
	Abstract:	Functions that directly support wa (requests from WACC and other wide area
				controlers).
				This is an extension of agent.go.

	Date:		6 Jan 2015
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"encoding/json"
	//"fmt"
	//"os"

	"codecloud.web.att.com/gopkgs/connman"
)


/*	Build a wa request and send to agent 
	The default wan_uuid is used if the WAN uuid isn't supplied by the requestor and comes
	in from the config file.
*/
func ( ad *agent_data ) send_wa_cmd( atype string, smgr *connman.Cmgr, pr *pend_req, def_wan_uuid *string ) ( ok bool ) {
	var (
		parm_map map[string]string
		host	string	
	)

	if pr.req.Req_data != nil {
		switch atype {
			case "wa_port":
				port_data := pr.req.Req_data.( *wa_port_req )			// get the port request information (token, project, subnet )
				parm_map = port_data.To_map()							// convert to map to pass to agent as parms
				if parm_map["wan_uuid"] == "" {
					parm_map["wan_uuid"] = *def_wan_uuid
				}

				host = *port_data.host
	
			case "wa_tunnel":
				tun_data := pr.req.Req_data.( *wa_tunnel_req )
				parm_map = tun_data.To_map()
				host = *tun_data.host
		
			case "wa_route":
				route_data := pr.req.Req_data.( *wa_route_req )			// get the port request information (token, project, subnet )
				parm_map = route_data.To_map()
				host = *route_data.host
		}
	}

	if parm_map == nil {
		am_sheep.Baa( 0, "WRN: unable to create wa agent command: missing data or bad type.  [TGUAGTXXX]" )		// FIX message id
		return false
	}

	msg := &agent_cmd{ Ctype: "action_list" }				// create command struct then convert to json
	msg.Actions = make( []action, 1 )
	msg.Actions[0].Atype = atype
	msg.Actions[0].Aid = pr.id
	msg.Actions[0].Hosts = make( []string, 1 )
	msg.Actions[0].Hosts[0] = host
	msg.Actions[0].Data = parm_map

	jmsg, err := json.Marshal( msg )			// bundle into a json string

	if err == nil {
		am_sheep.Baa( 1, "sending %s request id=%d  json=%s", atype, pr.id, jmsg  )
		ad.sendbytes2one( smgr, jmsg )
	} else {
		am_sheep.Baa( 0, "WRN: unable to create %s command: %s  [TGUAGTXXX]", atype, err )		// FIX message id
		return false
	}

	return true
}


