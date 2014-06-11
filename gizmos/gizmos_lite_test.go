// vi: sw=4 ts=4:

/*

	Mnemonic:	gizmos_lite_test
	Abstract:	builds tests to test the lite things
	Date:		28 April 2014
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

	"forge.research.att.com/tegu/gizmos"
)

const (
	time_12am int64 = 1357016400			// various timestamps for setting windows
	time_1am int64 = 1357020000
	time_5am int64 = 1357034400
	time_12pm int64 = 1357059600
	max_cap int64 = 100000
)

/*
	Test the ability to load links from a file
*/
func TestLoadLinks( t *testing.T ) {			// must use bloody camel case to be recognised by go testing 


	
	links, err := gizmos.Read_json_links( "static_links.json" ) 
	if err == nil  {
		fmt.Fprintf( os.Stdout, "Successful %d\n", len( links ) )
		for i := range links {
			fmt.Fprintf( os.Stdout, "link: %s/%d-%s/%d\n", links[i].Src_switch, links[i].Src_port, links[i].Dst_switch, links[i].Dst_port )
		}
	} else {
		fmt.Fprintf( os.Stdout, "failed to read links: %s  [FAIL]\n", err )
		t.Fail()
	}
}
