#!/bin/ksh
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ----------------------------------------------------------------------------------------
#
#	Mnemonic:	tegu_xapi
#	Abstract: 	This command is used to manipulate the objects contained within the Tegu
#			Extended API.  Currently, that includes Groups, Flow Classifiers (FCs),
#			and Chains, all used for scalable flow steering.  Other Tegu actions (the
#			"classic" API) are still controlled via tegu_req.
#
#			Requires that python-openstackclient package be installed.
#
#	Date:		14 Sep 2015
#	Author:		Robert Eby
#
#	Mods:		14 Sep 2015 - Created.
#
# ----------------------------------------------------------------------------------------

function usage {
	cat <<EOF

    usage: $argv0 command [ options ] [ objid ]

    Command is one of:
        group-create, group-delete, group-list, group-show, group-update
        fc-create, fc-delete, fc-list, fc-show, fc-update
        chain-create, chain-delete, chain-list, chain-show, chain-update
        help

    Options are:
        -h or --host host:port      Specify the host and port to use to contact Tegu, when Tegu
                                    is running on a different host than localhost:29444
        -I or --id                  Print only ID's from the returned JSON, rather than full JSON
                                    (on create and list operations).
        -t or --token token         Provide the keystone token to be passed to Tegu.
                                    Normally $argv0 will generate a new token.
        -v or --verbose             Display the curl commands used to talk to Tegu
        --help                      Print this message
        --name name                 Specify a name for an object for create or update
        --description descr         Specify text to describe an object for create or update
        --start-time time           Specify a start time for chain-create or chain-update
        --end-time time             Specify an end time for chain-create or chain-update
        --classifiers list          Specify a list of flow classifier IDs for chain-create or chain-update
        --groups list               Specify a list of group IDs for chain-create or chain-update
        --protocol proto            Specify the (numeric) protocol for fc-create
        --source-port port          Specify the (numeric) source port for fc-create
        --destination-port port     Specify the (numeric) destination port for fc-create
        --source-ip-address ip      Specify the source IP address for fc-create
        --destination-ip-address ip Specify the destination IP address for fc-create
        --subnet-id subnet          Specify the subnet ID to use for group-create
        --port-specs specs          Specify the port specifications to use for group-create or group-update

    Objid is the ID of the object being manipulated, if the command requires it.
    This is required for delete, show, and update operations.
EOF
}

function tolist {
	t2=
	sep=""
	for i
	do
		t2="$t2$sep \"$i\""
		sep=","
	done
	echo $t2
}

function build_json_request {
	t="\"api\":  \"tegu_xapi\""
	[ -n "$NAME" ] 		&& t="$t, \"name\": \"$NAME\""
	[ -n "$DESCR" ]		&& t="$t, \"description\": \"$DESCR\""
	[ -n "$STIME" ]		&& t="$t, \"start_time\": \"$STIME\""
	[ -n "$ETIME" ]  	&& t="$t, \"end_time\": \"$ETIME\""
	[ -n "$FCS" ]	    && t="$t, \"flow_classifiers\": [ $(tolist $FCS) ]"
	[ -n "$GROUPS" ]	&& t="$t, \"port_groups\": [ $(tolist $GROUPS) ]"
	[ -n "$PROTO" ]	    && t="$t, \"protocol\": $PROTO"
	[ -n "$SRC_PORT" ]  && t="$t, \"src_port\": $SRC_PORT"
	[ -n "$DST_PORT" ]	&& t="$t, \"dest_port\": $DST_PORT"
	[ -n "$SRC_IP" ]	&& t="$t, \"source_ip\": \"$SRC_IP\""
	[ -n "$DST_IP" ]	&& t="$t, \"dest_ip\": \"$DST_IP\""
	[ -n "$SUBNET_ID" ]	&& t="$t, \"subnet_id\": \"$SUBNET_ID\""
	[ -n "$PORTSPECS" ]	&& t="$t, \"port_specs\": [ $(tolist $PORTSPECS) ]"
	echo "{ $t }"
}

function postprocess {
	grep '"id"' | sed -e 's/.,//' -e 's/.* .//'
}

argv0="${0##*/}"
PATH=/bin:/usr/bin
HOST=localhost:29444
POSTPROC=cat
VERBOSE=:
while [[ "$#" -gt 0 ]]
do
	if [ -z "$CMD" ]
	then
		CMD="$1"
	else
		case "$1" in
		-h|--host) 		HOST="$2"; shift;;
		-I|--id)		POSTPROC=postprocess;;
		-t|--token)		TOKEN="$2"; shift;;
		-v|--verbose)	VERBOSE="set -x";;
		--name)			NAME="$2"; shift;;
		--description)	DESCR="$2"; shift;;
		--start-time)	STIME="$2"; shift;;
		--end-time)		ETIME="$2"; shift;;
		--classifiers)	FCS="$2"; shift;;
		--groups)		GROUPS="$2"; shift;;
		--protocol)		PROTO="$2"; shift;;
		--source-port)				SRC_PORT="$2"; shift;;
		--destination-port)			DST_PORT="$2"; shift;;
		--source-ip-address)		SRC_IP="$2"; shift;;
		--destination-ip-address)	DST_IP="$2"; shift;;
		--subnet-id)				SUBNET_ID="$2"; shift;;
		--port-specs)				PORTSPECS="$2"; shift;;
		*)							OBJ="$1";;
		esac
	fi
	shift
done
OBJTYPE=$(echo $CMD | sed s/-.*//)

# Get a token for curl if one is not provided.
if [ -z "$TOKEN" ]
then
	TOKEN=$(openstack token issue -f value -c id)
fi

case "$CMD" in
group-delete|fc-delete|chain-delete|group-show|fc-show|chain-show|group-update|fc-update|chain-update)
	if [ -z "$OBJ" ]
	then
		echo $argv0: ID needed for the $CMD command.
		exit 1
	fi
	;;
esac

$VERBOSE
case "$CMD" in
group-create|fc-create|chain-create)
	data=$(build_json_request)
	curl -X POST -s -d "$data" -H "X-Auth-Tegu: $TOKEN" -H "User-Agent: tegu_xapi" -H "Accept: application/json" \
		http://$HOST/tegu/$OBJTYPE/ | $POSTPROC
	;;

group-delete|fc-delete|chain-delete)
	curl -X DELETE -s -H "X-Auth-Tegu: $TOKEN" -H "User-Agent: tegu_xapi" http://$HOST/tegu/$OBJTYPE/$OBJ/
	;;

group-list|fc-list|chain-list)
	curl -X GET -s -H "X-Auth-Tegu: $TOKEN" -H "User-Agent: tegu_xapi" http://$HOST/tegu/$OBJTYPE/ | $POSTPROC
	;;

group-show|fc-show|chain-show)
	curl -X GET -s -H "X-Auth-Tegu: $TOKEN" -H "User-Agent: tegu_xapi" http://$HOST/tegu/$OBJTYPE/$OBJ/
	;;

group-update|fc-update|chain-update)
	data=$(build_json_request)
	curl -X PUT -s -d "$data" -H "X-Auth-Tegu: $TOKEN" -H "User-Agent: tegu_xapi" -H "Accept: application/json" \
		http://$HOST/tegu/$OBJTYPE/$OBJ/
	;;

help)
	usage
	;;

*)
	echo $argv0: unknown command $CMD
	usage
	exit 1
	;;
esac
exit 0
