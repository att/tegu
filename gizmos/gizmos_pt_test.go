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

	Mnemonic:	gizmos_pledge_pt_test
	Abstract:	test some of the pasthrough pledge functions.
	Date:		26 March 2015
	Author:		E. Scott Daniels

*/

package gizmos_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/att/tegu/gizmos"
)

const (
)

// ---- support -----------------------------------------------------------------------


// --- tests --------------------------------------------------------------------------
func TestPass_pledge( t *testing.T ) {

	fmt.Fprintf( os.Stderr, "\n------- make passthrou pledge -----------\n" )
	host := "host"
	port := "7890"
	commence := time.Now().Unix() + 3600
	expiry := int64( commence + 45 )
	id := "res-test-pass-pledge"
	ukey := "my-cookie"
	
	ppt1, err := gizmos.Mk_pass_pledge( &host, &port, commence, expiry, &id, &ukey )
	if err != nil {
		fmt.Fprintf( os.Stderr, "cannot make pass pledge; all other passthrough tests aborted: %s	[FAIL]\n", err )
		t.Fail()
		return
	}
	fmt.Fprintf( os.Stderr, "mk successful\n" );

	pptc := ppt1.Clone( "cloned" )
	if pptc == nil {
		fmt.Fprintf( os.Stderr, "cannot clone pass pledge; all other passthrough tests aborted	[FAIL]\n" )
		t.Fail()
		return
	}
	fmt.Fprintf( os.Stderr, "clone successful\n" );
	
	host2 := "host2"
	port2 := "7890"
	id2 := "res-test-pass-pledge2"
	ukey2 := "my-cookie"
	ppt2, err := gizmos.Mk_pass_pledge( &host2, &port2, commence, expiry, &id2, &ukey2 ) 
	if err != nil {
		fmt.Fprintf( os.Stderr, "cannot make second pass pledge; all other passthrough tests aborted: %s	[FAIL]\n", err )
		t.Fail()
		return
	}

	gp := gizmos.Pledge( pptc )			// must convert to a generic pledge so we can take address off next
	if ppt1.Equals( &gp ) {
		fmt.Fprintf( os.Stderr, "clone reports equal [OK]\n" )
	} else {
		fmt.Fprintf( os.Stderr, "clone reports !equal [FAIL]\n" )
	}

	gp = gizmos.Pledge( ppt2 )			// must convert to a generic pledge so we can take address off next
	if ppt1.Equals(  &gp ) {
		fmt.Fprintf( os.Stderr, "second pledge reports equal [FAIL]\n" )
	} else {
		fmt.Fprintf( os.Stderr, "second pledge reports !equal [OK]\n" )
	}


	fmt.Fprintf( os.Stderr, "json:   %s\n", ppt1.To_json() )
	fmt.Fprintf( os.Stderr, "string: %s\n", ppt1 )
	fmt.Fprintf( os.Stderr, "chkpt: %s\n", ppt1.To_chkpt() )

}
