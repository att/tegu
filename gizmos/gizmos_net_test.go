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

	Mnemonic:	gizmos_net_test
	Abstract:	Builds a test network and runs some of the path finding functions to verify
	Date:		10 June 2014
	Author:		E. Scott Daniels

*/

package gizmos_test

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	"os"
	//"strings"
	//"time"
	"testing"

	"github.com/att/tegu/gizmos"
)

const (
)

/*
	Test some network pathfinding. Reads a topo from the static json file test_net.json and builds
	a network of hosts and links, then attempts to find all paths between them using the switch
	find all functions.
*/
func TestNet( t *testing.T ) {			// must use bloody camel case to be recognised by go testing

	var (
		fsw	*gizmos.Switch
		sw_list	map[string]*gizmos.Switch
	)

	sw_list = make( map[string]*gizmos.Switch )
	fmt.Fprintf( os.Stderr, "\n------------- net test starts -----------------\n" )
	fname := "static_links.json"
	links, err := gizmos.Read_json_links( &fname )
	if err == nil  {
		fmt.Fprintf( os.Stderr, "read  %d links from the file\n", len( links ) )
	} else {
		fmt.Fprintf( os.Stderr, "failed to read links: %s  [FAIL]\n", err )
		t.Fail()
		return
	}
	
	last := ""
	fsw = nil
	for i := range links {									// parse all links returned from the controller
		ssw := sw_list[links[i].Src_switch]
		if ssw == nil {
			ssw = gizmos.Mk_switch( &links[i].Src_switch )		// source switch
			sw_list[links[i].Src_switch] = ssw
		}
	
		dsw := sw_list[links[i].Dst_switch]
		if dsw == nil {
			dsw = gizmos.Mk_switch( &links[i].Dst_switch )		// dest switch
			sw_list[links[i].Dst_switch] = dsw
		}

		l := gizmos.Mk_link( ssw.Get_id(), dsw.Get_id(), 100000000, 95, nil );		// link in forward direction
		l.Set_forward( dsw )
		l.Set_backward( ssw )
		ssw.Add_link( l )

		l = gizmos.Mk_link( dsw.Get_id(), ssw.Get_id(), 100000000, 95, nil );		// link in backward direction
		l.Set_forward( ssw )
		l.Set_backward( dsw )
		dsw.Add_link( l )

		mac := fmt.Sprintf( "00:00:00:00:00:%02d", i )
		ip := fmt.Sprintf( "10.0.0.%02d", i )
		h := gizmos.Mk_host( mac, ip, "" )
		h.Add_switch( ssw, i )				// add a host to each src switch
		vmname := "foobar-name"
		ssw.Add_host( &ip, &vmname, i+200 )
		fmt.Fprintf( os.Stderr, "adding host: %s\n", ip )

		if fsw == nil {					// save first switch to use as start of search
			fsw = ssw
		}

		mac = fmt.Sprintf( "%02d:00:00:00:00:00", i )
		ip = fmt.Sprintf( "10.0.0.1%02d", i )
		h = gizmos.Mk_host( mac, ip, "" )
		h.Add_switch( dsw, i )				// add a host to each dest switch
		vmname2 := "foobar-name2"
		dsw.Add_host( &ip, &vmname2, i+200 )

		fmt.Fprintf( os.Stderr, "adding host: %s\n", ip )
		last = ip
	}

	fmt.Fprintf( os.Stderr, ">>> searching for: %s\n", last );
	usrname := "username"
	fsw.All_paths_to( &last, 0, 0, 100, &usrname, 95 )
}

