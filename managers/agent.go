// vi: sw=4 ts=4:

/*

	Mnemonic:	agent
	Abstract:	Manages everything associated with agents. Listens on the well known channel
				for requests from other tegu threads, and manages a separate data channel
				for agent input (none expected at this time.

	Date:		30 April 2014
	Author:		E. Scott Daniels

	Mods:		
*/

package managers

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	//"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	"os"
	//"strings"
	//"time"

	"forge.research.att.com/gopkgs/bleater"
	//"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/connman"
	"forge.research.att.com/gopkgs/ipc"
	//"forge.research.att.com/tegu"
	//"forge.research.att.com/tegu/gizmos"
)

/*
	Someday we might need more info per agent, so set up to allow that now.
*/
type agent struct {
	id		string
}

type agent_data struct {
	agents	map[string]*agent	// hash for direct index
}


/*
	Build an agent and add to our list of agents.
*/
func (ad *agent_data) Mk_agent( aid string ) ( na *agent ) {

	na = &agent{}
	na.id = aid
	ad.agents[na.id] = na

	return
}

/*
	Send the message to one agents.
	TODO: randomise this a bit. 
*/
func (ad *agent_data) send2one( smgr *connman.Cmgr,  msg string ) {
	for id := range ad.agents {
		smgr.Write( id, []byte( msg ) )
		return
	}
}

/*
	Send the message to all agents.
*/
func (ad *agent_data) send2all( smgr *connman.Cmgr,  msg string ) {
	for id := range ad.agents {
		smgr.Write( id, []byte( msg ) )
	}
}

// ---------------- main agent goroutine -----------------------------------------------------------

func Agent_mgr( ach chan *ipc.Chmsg ) {
	var (
		port	string = "29055"						// port we'll listen on for connections
		adata	*agent_data
	)

	adata = &agent_data{}
	adata.agents = make( map[string]*agent )

	am_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	am_sheep.Set_prefix( "agentmgr" )
	tegu_sheep.Add_child( am_sheep )					// we become a child so that if the master vol is adjusted we'll react too

														// suss out config settings from our section
	if p := cfg_data["agent"]["port"]; p != nil {
		port = *p
	}

														// enforce some sanity on config file settings
	net_sheep.Baa( 1,  "agent_mgr thread started: listening on port %d", port )

	sess_chan := make( chan *connman.Sess_data, 1024 )		// channel for data from agents, conns, discs etc
	smgr := connman.NewManager( port, sess_chan );
	

	for {
		select {							// wait on input from either channel
			case req := <- ach:
				req.State = nil				// nil state is OK, no error

				am_sheep.Baa( 3, "processing request %d", req.Msg_type )

				switch req.Msg_type {
					case REQ_NOOP:			// just ignore -- acts like a ping if there is a return channel

					case REQ_SENDALL:
						if req.Req_data != nil {
							adata.send2all( smgr,  req.Req_data.( string ) )
						}

					case REQ_SENDONE:
						if req.Req_data != nil {
							adata.send2one( smgr,  req.Req_data.( string ) )
						}
				}

				am_sheep.Baa( 3, "processing request finished %d", req.Msg_type )			// we seem to wedge in network, this will be chatty, but may help
				if req.Response_ch != nil {				// if response needed; send the request (updated) back 
					req.Response_ch <- req
				}


			case sreq := <- sess_chan:		// data from the network
				switch( sreq.State ) {
					case connman.ST_ACCEPTED:		// newly accepted connection; no action 

					case connman.ST_NEW:			// new connection
						a := adata.Mk_agent( sreq.Id )
						am_sheep.Baa( 1, "new agent: %s [%s]", a.id, sreq.Data )
				
					case connman.ST_DISC:
						am_sheep.Baa( 1, "agent dropped: %s", sreq.Id )
						if _, not_nil := adata.agents[sreq.Id]; not_nil {
							delete( adata.agents, sreq.Id )
						} else {
							am_sheep.Baa( 1, "did not find an agent with the id: %s", sreq.Id )
						}
						
					case connman.ST_DATA:
						if _, not_nil := adata.agents[sreq.Id]; not_nil {
							am_sheep.Baa( 1, "data: [%s]  %d bytes ignored:  %s", sreq.Id, len( sreq.Buf ), sreq.Buf )
						} else {
							am_sheep.Baa( 1, "data from unknown agent: [%s]  %d bytes ignored:  %s", sreq.Id, len( sreq.Buf ), sreq.Buf )
						}
				}

		}			// end select
	}
}

