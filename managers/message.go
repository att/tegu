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

	Mnemonic:	message
	Abstract:	Unlike the other managers, this doesn't 'loop'; it only sets up the 
				messaging and returns.  The main uses it to ensure that messaing is set
				before starting the goroutines.  It's broken out to keep main simiple, and
				should we ever need to add any generic listeners, they can be added here.

	Date:		11 November 2015
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"os"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/ipc/msgrtr"
)


// -- configuration management -----------------------------------------------------------
/*
	All needed config information pulled from the config file.
*/
type msg_cfg struct {
	port	string				// port the message manager should listen on
	msgpath	string				// path after http:// that we're listing to
}

/*
	Suss out things we need from the config data and build a new struct.
*/
func mk_msg_cfg( cfg_data map[string]map[string]*string ) ( nc *msg_cfg ) {

	nc = &msg_cfg {
		port: "localhost:29445",
		msgpath: "tegu/events",
	}

	/*
	if cfg_data["default"] != nil {								// things we pull from the default section
	}
	*/

	if cfg_data["message"] != nil {								// our specific stuff
		if p := cfg_data["message"]["port"]; p != nil {
			nc.port = *p
		}	
		if p := cfg_data["message"]["port"]; p != nil {
			nc.port = *p
		}	
	}

	return
}

// ------------ private -------------------------------------------------------------------------------------


// --------- public -------------------------------------------------------------------------------------------

/*
	To be executed as a go routine.
	Starts the message router (package) and registeres the messages we want to listen to. 
*/
func Message_mgr( ) {

	msg_sheep := bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	msg_sheep.Set_prefix( "msgmgr" )
	tegu_sheep.Add_child( msg_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	cfg := mk_msg_cfg( cfg_data )

	if cfg == nil {
		msg_sheep.Baa( 0, "CRI: abort: unable to build a config struct for message goroutine" )
		os.Exit( 1 )
	}
	
	msgrtr.Start( cfg.port, cfg.msgpath, tegu_sheep )	// pass in the main sheep as ours goes away soon

	msg_sheep.Baa( 1,  "message manager thread started: http://%s/%s", cfg.port, cfg.msgpath )
}
