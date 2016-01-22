#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
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
# ---------------------------------------------------------------------------
#

#	Mnemonic:	ql_bw_fmods
#	Abstract:	Generates all needed flow-mods on an OVS for a bandwidth reservation
#				between src and dest VMs (-s and -d respectively). Src is considered
#				to be the local VM (the VM that is attached to the OVS, and dest is
#				the remote VM (-l and -r could be used, but src and dest are used in
#				the other agent scripts so this is being consistent).  In the case where
#				one of the VMs is a router, and an external IP address is supplied and
#				must be used as the flow-mod match criteria, an indication is needed
#				as to whether the the external IP address is "associated" with the
#				source or destination VM.  This is indicated by the presence of the -S
#				or -D option on the command line.
#
#				Bandwidth reservation flow mods are set up this way:
#					inbound
#						p450 Match:	
#								meta == 0 &&
#								vlan id &&
#								reservation VM0 &&
#								reservation VM1 [&& external IP]
#							 Action:
#								[strip dscp value]
#								set metadata 0x01 (resub 90)
#								resub 0 for openstack fmod application
#
#					outbound
#						p400 Match:
#								meta == 0 &&
#								inbound openflow port &&
#								reservation VM0 &&
#								reservation VM1 [&& external IP]
#							 Action:
#								mark with meta value (resub 90)
#								set dscp value
#								resub 0 to apply openstack fmods
#
#				In the brave new world of endpoints (using uuids rather than mac addresses from Tegu),
#				and to deal with the fact that openstack happily allows duplicate MAC addresses, we need
#				to do the following:
#					1) using the local (source) uuid, suss out the vlan id and the openflow port number
#					   from ovs. 
#					2) match outbound traffic based on the inbound openflow port/mac combo since that
#					   is unique where MAC might not be.
#					3) match inbound traffic based on the mac/vlan combination as that is unique
#					   where mac alone is not.
#
#				Transport protocol (tcp/udp) and ports are pesky things. The reservation may have one
#				associated with either endpoint.  The invocation of this script uses -p and -P to 
#				associate a protocol and port port number with the remote endpoint (-r) and the local
#				endpoint (-s).  For the local endpoint -P is used and -p is used if there is a proto/port
#				associated with the remote endpoint.  
#							
#	Date:		20 March 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:		22 Mar 2015 - Added keep on exit option.
#				27 Mar 2015 - Added ipv6 support.
#				20 Apr 2015 - Accept external IP direction
#				11 May 2015 - Inbound flow-mods must match all types, changed to allow for this.
#				14 May 2015 - To eliminate the use of br-rl and thus the last HTB queue. (flow-mods are
#								now very simple, one in each direction)
#				28 May 2015 - Added match vlan support (-V)
#				18 Jun 2015 - Better handling of -q allowing HTB shutoff to be affected completely by
#								agent scripts (Tegu still thinks it's being set!)
#				09 Oct 2015 - Added ability to xlate a neutron uuid into a mac/vlan/ofport tuple.
#				16 Oct 2015 - Tweaked protocol args on the send_ovs_fmod commands.
#				20 Oct 2015 - Correct bug that was not marking the protocol correctly (was putting on all src
#								or all dest on both inbound and outbound fmods rather than src for one and
#								dest for the other.
#				13 Nov 2015 - Now susses the bridge based on uuid from ovs rather than assuming br-int.
#				14 Jan 2016 - Fixed typos in comments/doc.
#				16 Jan 2016 - Added ability to set rate limiting flow-mods (yet again) if the bridge is
#								listed in the config file on the bandwidth:rate-limit variable.
#				18 Jan 2016 - Added ability to detect and properly handle a source TCP address (e.g.
#								token/project/endpoint/123.45.67.89[:port][{vlan}] can be supplied on the 
#								reservation to Tegu which yields -P {tcp|udp}[:address]:{port|0}. 
# ---------------------------------------------------------------------------------------------------------

function cleanup
{
	rm -f /tmp/PID$$.*
	exit $1	
}

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.1/15125"
	echo "usage: $argv0 [-6] [-d dest-endpt] [-E external-ip] [-h host] [-k] [-n] [-o] [-p|P proto[:addr]:port] [-s src-endpt] [-T dscp] [-t hard-timeout] [-v]"
	echo "usage: $argv0 [-X] 		# delete all"
	echo ""
	echo "  -6 forces IPv6 address matching to be set"
}

function suss_ovs_data
{
	if [[ ! -s $ovs_data ]]
	then
		ql_suss_ovsd >$ovs_data
	fi
}

# accept either a uuid or mac and returns the the mac, vlan-id and openflow port associated.
# guarenteed to be correct if the uuid (neutron) is passed in, and probably correct when
# given a mac unless there are two devices with the same mac address attached to the OVS
# which seems wrong, but possible.
# Output is echoed to stdout in this order: mac, vlan, port.
function uuid2mac 
{
	#CAUTION: don't write to stdout other than the data from awk
	suss_ovs_data

	if [[ $1 == *":"* ]]		# we assume that a uuid does not have colons
	then
		echo "$1 -1 0"			# no vlan or port if mac is passed in (we assume this is the remote side)
		return
	fi

	#ql_suss_ovsd | grep $1 | awk '{ print $5, $7, $3, $8 }'		#  mac, vlan, port, bridge
	grep "$1" ${ovs_data:-/dev/null} | awk '{ print $5, $7, $3, $8 }'		#  mac, vlan, port, bridge
}

# given a bridge name, find the qosirl0 port number (port that attaches a main bridge to the rate limiting bridge)
# port is written on stdout for caller
function get_rlport
{
	#CAUTION: don't write to stdout other than the port from awk

	if [[ -z $1 ]]
	then
		echo "-1"
		return
	fi

	suss_ovs_data

	awk -v bridge="$1" '
		BEGIN { port = -1 }
		/^port: / &&  $8 == bridge  && $4 == "qosirl0" {
			port = $3
			exit( 0 )
		}
		END {
			printf( "%d\n", port )				# just out the port
		}
	' $ovs_data
}

# accept proto:[address:]port and split them echoing three strings: proto, addr, port
# if address is missing the string 'none' is echoed
function split_pap
{
	typeset proto=${1%%:*}		# strip off protocol
	typeset rest=${1#*:}		# address:port or just port

	case $rest in 
		\[*\]:)					# ipv6 address:port
			typeset addr="${rest%%]*}"
			ip_type="-6"								# must force the type
			echo "$proto ${addr##*\[} ${rest##*]:}"
			;;

		\[*)					# ipv6 address
			typeset addr="${rest%%]*}"
			echo "$proto ${addr##*\[} 0"
			;;

		*.*.*.*:)					# ipv4:port
			echo "$proto ${rest%%:*} ${rest##*:}"
			;;

		*.*.*.*)					# ipv4
			echo "$proto ${rest%%:*} 0"
			;;

		*)						# just port
			echo "$proto none $rest"
			;;
	esac
}

# ----------------------------------------------------------------------------------------------------------

config="${TEGU_AGENT_CONFIG:-tegu_agent.cfg}"
ovs_data=/tmp/PID$$.ovsd		# output from ovs_susd

bridge="br-int"
mt_base=90				# meta table base 90 sets 0x01, 91 sets 0x02, 94 sets 0x04...

lmac=""					# local mac 	src outbound, dest inbound
rmac=""					# remote mac	src inbound, dest outbound
queue=""
idscp=""
odscp=""
host=""
forreal=""
pri_base=0				# priority is bumpped up a bit for protocol specific f-mods
vp_base=0				# priority added if vlan match supplied (outbound)
one_switch=0			# may need to handle things differently if one switch is involved
koe=0					# keep dscp value as packet 'exits' our environment. Set if global_* traffic type given to tegu
to_value="61"			# value used to check (without option flag)
timout="-t $to_value"	# timeout parm given on command
operation="add"			# -X allows short time durations for deletes
ip_type="-4"			# default to forcing an IP type match for outbound fmods; inbound fmods do NOT use this
ex_local=1				# the external IP is "associated" with the local when 1 (-S) and with the remote when 0 (-D)
set_vlan=1				# -v causes all vlan processing to be ignored

ob_lproto=""			# out/inbound local protocol Set with -P
ib_lproto=""
ob_rproto=""			# out/inbound remote proto set sith -p
ib_rproto=""

ob_lproto=""            # out/inbound local protocol Set with -P
ib_lproto=""
ob_rproto=""            # out/inbound remote proto set sith -p
ib_rproto=""

typeset -C bandwidth											# must ensure this is set to handle missing config file
ql_parse_config -f $config >/tmp/PID$$.cfg && . /tmp/PID$$.cfg		# xlate config file into variables and source them
bandwidth.cookie=${bandwidth.cookie:-0xb0ff}					# default if not in config or config missing

while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;								# force ip6 option to be given to send_ovs_fmod (outbound only).
		-b)		mt_base="$2"; shift;;
		-d)		uuid2mac "$2" | read rmac dvlan dofport dbridge junk; shift;;	# get the mac, vlan, bridge and openflow port from ovs based on neutron uuid
		-D)		ex_local=0;;								# external IP is "associated" with the rmac (-d) address
		-E)		exip="$2"; shift;;
		-h)		host="-h $2"; shift;;
		-k)		koe=1;;

		-n)		forreal="-n"
				trace_on="set -x"
				trace_off="set +x"
				;;

		-o)		one_switch=1;;

		-P)		pri_base=5;									# source proto:[addr:]port priority must increase to match over more generic f-mods
				split_pap "$2" | read proto addr port junk	# expect {tcp|udp}:[address:]port
				if [[ $addr != "none" ]]
				then
					ob_netaddr="-S $addr"					# transport protocol address
					ib_netaddr="-D $addr"
				fi
				ob_rproto="-P $proto:${port:-0}"			# proto matches -d endpoint (remote), proto for outbound dest(P), inbound src(p)
				ib_rproto="-p $proto:${port:-0}" 
				shift
				;;		

		-p)		pri_base=5	 								# dest proto:port priority must increase to match over more generic f-mods
				split_pap "$2" | read proto addr port junk	# expect {tcp|udp}:[address:]port
				if [[ $addr != "none" ]]
				then
					ib_netaddr="-S $addr"					# transport protocol address
					ob_netaddr="-D $addr"
				fi
				ob_lproto="-p $proto:${port:-0}" 			# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
				ib_lproto="-P $proto:${port:-0}" 			# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
				shift
				;;		

		-q)		queue="-q $2"; shift;;					# soon to change to meter
		-s)		uuid2mac "$2" | read lmac svlan sofport sbridge junk; shift;;	# get the mac, vlan and openflow port from ovs based on neutron uuid
		-S)		ex_local=1;;								# external IP is "associaetd" with the lmac (-s) address.
		-t)		to_value=$2; timeout="-t $2"; shift;;
		-T)		odscp="-T $2"; shift;;
		-v)		set_vlan=0;;							# ignored -- maintained for backwards compat
		-V)		vp_base=5; match_vlan="-v $2"; shift;;	# vlan id given on resrvation for match (applies only to outbound)
		-X)		operation="del";;

		-\?)	usage
				cleanup 0
				;;

		*)	echo "unrecognised option: $1"
			usage
			cleanup 1
			;;
	esac

	shift
done


match_port=""
if (( sofport > 0 ))				# source should be local and what we have, so use it first
then
	vp_base=5
	match_port="-i $sofport"		# for outbound fmod
	match_vlan="-v $svlan"			# for inbound fmod
	logit "openflow vlan/port ($svlan, $sofport) captured from source uuid"
else
	if (( dofport > 0 ))			# if caller got them backwards
	then
		vp_base=5
		match_port="-i $dofport"		# for outbound fmod
		match_vlan="-v $dvlan"			# for inbound fmod
		logit "openflow vlan/port ($dvlan, $dofport) captured from dest uuid"
	else
		logit "openflow port and vlan information not found"
	fi
fi

if [[ -n sbridge ]]
then
	logit "setting bridge based on sbridge: $sbridge"
	bridge=$sbridge
fi

# CAUTION:  this is confusing, so be careful (see notes in flower box at top)
if [[ -n $exip ]]						# need to set up matching for external
then
	if [[ $exip == "["*"]" ]]
	then
		ip_type="-6"					# force ip type to v6
		exip="${exip#*\[}"
		exip="${exip%\]*}"
	fi
	if (( ex_local ))					# the lmac is associated with the external IP address
	then
		oexip="-S $exip"				# for outbound, the external ip is the src
		iexip="-D $exip"				# for inbound the external ip is the dest
	else
		oexip="-D $exip"				# rmac is associated with external IP, thus outbound external IP is dest
		iexip="-S $exip"				# and inbound the external is source.
	fi
else
	oexip=""
	iexip=""
fi

if [[ -z $lmac || -z $rmac ]]
then
	logit "must have source and dest mac addresses in order to generate flow-mods   [FAIL]"
	rm -f /tmp/PID$$.*
	cleanup 1
fi

if [[ -z $bridge ]]
then
	logit "flow_mods not set: unable to determine bridge"
	cleanup 1
fi

if (( koe ))			# true == keep dscp marking as packet 'exits' our control area
then
	idscp=""			# don't reset the dscp value on inbound (exiting) traffic
else
	idscp="-T 0"		# keep off, force marking to 0
fi

if (( ! one_switch )) && [[ -n ${bandwidth.rate_limit} ]]  &&  [[ " ${bandwidth.rate_limit} "  ==  *" $bridge "* ]]				
then 			# if this bridge is flagged for rate limiting, push all traffic through the rate limiting extension bridge
	rl_port=$( get_rlport $bridge )
	to_rl=" -o $rl_port"					# to/from parms for fmod
	fr_rl=" -i $rl_port"

	# inbound
	#	dest is endpoint & traffic NOT from the rl bridge: push traffic on the bridge (no queue)
	#	dest is endpoint & input port is the rl bridge: output traffic to the endpoint's port
	#
	# outbound
	#	source is endpoint/IP pair & traffic is NOT from the rl bridge: push traffic on the rl bridge with queue
	#	source is endpoint & traffic is NOT from the rl bridge: push on rl bridge q==0
	#	source is endpoint & from rl bridge NORMAL processing

	####	send_ovs_fmod $forreal $host $timeout -p xxx  --match $match_vlan $ip_type -m 0x0/0x7 $iexip -d $lmac -s $rmac $ib_lproto $ib_rproto $ib_netaddr --action $queue $idscp -M 0x01 -R ,0 -N $operation ${bandwidth.cookie} $bridge
	echo "not ready for this quite yet :)"


	#--send_ovs_fmod      -p 850 -t 180 --match -m 0/0x7 -i 55 -s  fa:de:ad:f8:d0:0e  --action   -M 0x01 -R ,0 -N  add 0xffee br-int
	# match all traffic off of the rate limiting bridge and route normally
	if ! send_ovs_fmod	$forreal  -p 800 $timeout --match -m 0/0x7 -i $rl_port --action   -M 0x01 -R ,0 -N  add ${bandwidth.cookie} $bridge			# anything off of rl bridge is send normally
	then
		logit "could not set rate limiting p 850 (inbund from br0rl) flowmod	[FAIL]"
		cleanup 1
	fi

    # route all arp traffic 'normally'
    if ! send_ovs_fmod     $forreal -p 550 $timeout --match -a -s $lmac -m 0/0x07 --action  -M 0x01 -R ,0  add ${bandwidth.cookie} $bridge 			# ensure all arp doesn't hit rl
	then
		logit "could not set rate limiting p 550 flowmod	[FAIL]"
		cleanup 1
	fi


#TODO -- add queue
	# force the reservation traffic to the rate limiting bridge after setting the vlan
    if ! send_ovs_fmod     $forreal -p $(( pri_base + 400 )) $timeout --match -s $lmac -d $rmac -m 0/0x07 $ip_type $iexip  $ib_lproto $ib_rproto $ib_netaddr --action  -v $svlan -o $rl_port  add ${bandwidth.cookie} $bridge			#res traffic over bridge
	then
		logit "could not set rate limiting p 400 (out to br-rl) flowmod	[FAIL]"
		cleanup 1
	fi

	# must push anything inbound to the endpoint  directly as the switch likely things it lives on br-rl
	if ! send_ovs_fmod      $forreal -p 500 $timeout --match -v $svlan  -d $lmac -m 0/0x07 --action  -V -o $lmac  add ${bandwidth.cookie} $bridge		# send_ovs_fmod allows late binding on -o
	then
		logit "could not set rate limiting p 500 (inbound direct) flowmod	[FAIL]"
		cleanup 1
	fi
	
else
	# CAUTION:	action options to send_ovs_fmods are probably order dependent, so be careful.
	# DANGER:	NEVER set queues here, only if bridge is rate limiting and above code will handle
	if (( ! one_switch ))
	then
		# inbound -- only if they are not on the same switch
		$trace_on
		send_ovs_fmod $forreal $host $timeout -p $(( 450 + pri_base )) --match $match_vlan $ip_type -m 0x0/0x7 $iexip -d $lmac -s $rmac $ib_lproto $ib_rproto $ib_netaddr --action $idscp -M 0x01 -R ,0 -N $operation ${bandwidth.cookie} $bridge
		rc=$?
		$trace_off
	else
		if (( ! koe ))		# one switch and keep is off, no need to set dscp on the outbound fmod
		then
			odscp=""
		fi
	fi

	#outbound
	if [[ -n $match_port ]]			# if we have an input port, we can drop the source mac match (local mac)
	then							
		ob_smac=""
	else
		ob_smac="-s $lmac"
	fi
	
	$trace_on
	send_ovs_fmod $forreal $host $timeout -p $(( 400 + vp_base + pri_base )) --match  $match_port $ip_type -m 0x0/0x7 $oexip $ob_smac -d $rmac $ob_lproto $ob_rproto $ob_netaddr --action $odscp -M 0x01  -R ,0 -N $operation ${bandwidth.cookie} $bridge
	(( rc = rc + $? ))
	$trace_off
fi

cleanup $(( rc > 0 ))			# exit 1 if any number of failures earlier
