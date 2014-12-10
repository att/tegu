// vi: sw=4 ts=4:

/*
------------------------------------------------------------------------------------------------
	Mnemonic:	flowmod
	Abstract:	Manages flowmod related structures for communicating with the agent.
	Date:		09 May 2014
	Authors:	E. Scott Daniels, Matti Hiltnuen, Kaustubh Joshi

	Modifed:	
------------------------------------------------------------------------------------------------
*/

package gizmos

import (
	"encoding/json"
)


// these values are all public so that we can use the json marshal() function to build the json
type fmod_match struct {
	Src_mac	string
	Dest_mac	string
	Dscp	int
}

type fmod_action struct {
	Dscp	int
	Queue	int
}

type fmod struct {
	Priority	int
	Timeout		int
	Cookie		int
	Swname		string			// switch
	
	Match		*fmod_match
	Action		*fmod_action
}

/*
	Create a generic struct from basic data
*/
func Mk_flow_mod( priority int, timeout int, cookie int, swname string ) ( fm *fmod ) {
	fm = &fmod{
		Priority:	priority,
		Timeout:	timeout,
		Cookie:		cookie,
		Swname:		swname,
	}

	fm.Match = &fmod_match{}
	fm.Action = &fmod_action{}

	return
}

/*
	Set one or both values in the action. If either value is less than zero
	then the cooresponding value in the action is unchanged.
*/
func (fm *fmod) Set_action( dscp int, queue int ) {
	if fm != nil {
		if dscp >= 0  {
			fm.Action.Dscp = dscp
		}
		if queue >= 0  {
			fm.Action.Queue = queue
		}
	}
}

/*
	Set values in the match. If any values are < 0, or an empty string, the 
	value is not changed. 
*/
func (fm *fmod) Set_match( src_mac string, dest_mac string, dscp int ) {
	if fm != nil {
		if dscp >= 0  {
			fm.Match.Dscp = dscp
		}
		if src_mac != "" {
			fm.Match.Src_mac = src_mac
		}
		if dest_mac != "" {
			fm.Match.Dest_mac = dest_mac
		}
	}
}

func (fm *fmod) To_json( ) ( jbytes []byte, err error ) {
	jbytes, err = json.Marshal( fm )

	return	
}
