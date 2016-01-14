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
#					1) using the local (source) uuid, suss out the vland and the openflow port number
#					   from ovs. 
#					2) match outbound traffic based on the inbound openflow port/mac combo since that
#					   is unique where MAC might not be.
#					3) match inbound traffic based on the mac/vlan combination as that is uniqueue
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
# ---------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.1/15125"
	echo "usage: $argv0 [-6] [-d dst-mac] [-E external-ip] [-h host] [-k] [-n] [-o] [-p|P proto:port] [-s src-mac] [-T dscp] [-t hard-timeout] [-v]"
	echo "usage: $argv0 [-X] # delete all"
	echo ""
	echo "  -6 forces IPv6 address matching to be set"
}

# accept either a uuid or mac and returns the the mac, vlan-id and openflow port associated.
# guarenteed to be correct if the uuid (neutron) is passed in, and probably correct when
# given a mac unless there are two devices with the same mac address attached to the OVS
# which seems wrong, but possible.
# Output is echoed to stdout in mac, vlan, port order.
function uuid2mac 
{
	if [[ $1 == *":"* ]]		# we assume that a uuid does not have colons
	then
		echo "$1 -1 0"			# no vlan or port if mac is passed in (we assume this is the remote side)
		return
	fi

	ql_suss_ovsd | grep $1 | awk '{ print $5, $7, $3, $8 }'		#  mac, vlan, port, bridge
}

# ----------------------------------------------------------------------------------------------------------

cookie="0xb0ff"			# static for now, but might want to make them user controlled, so set them up here
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
queue=""
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


while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;							# force ip6 option to be given to send_ovs_fmod (outbound only).
		-b)		mt_base="$2"; shift;;
		-d)		uuid2mac "$2" | read rmac dvlan dofport dbridge junk; shift;;	# get the mac, vlan, bridge and openflow port from ovs based on neutron uuid
		-D)		ex_local=0;;								# external IP is "associated" with the rmac (-d) address
		-E)		exip="$2"; shift;;
		-h)		host="-h $2"; shift;;
		-k)		koe=1;;
		-n)		forreal="-n";;
		-o)		one_switch=1;;

		-P)		pri_base=5;								# source proto:port priority must increase to match over more generic f-mods
				ob_rproto="-P $2"  						# proto matches -d endpoint (remote), proto for outbound dest(P), inbound src(p)
				ib_rproto="-p $2" 
				shift
				;;		

		-p)		pri_base=5 								# dest proto:port priority must increase to match over more generic f-mods
				ob_lproto="-p $2" 						# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
				ib_lproto="-P $2" 						# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
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
				exit 0
				;;

		*)	echo "unrecognised option: $1"
			usage
			exit 1
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
	logit "openflow vland/port ($svlan, $sofport) captured from source uuid"
else
	if (( dofport > 0 ))			# if caller got them backwards
	then
		vp_base=5
		match_port="-i $dofport"		# for outbound fmod
		match_vlan="-v $dvlan"			# for inbound fmod
		logit "openflow vland/port ($dvlan, $dofport) captured from dest uuid"
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
	exit 1
fi

if (( koe ))
then
	idscp=""			# don't reset the dscp value on inbbound (exiting) traffic
else
	idscp="-T 0"
fi

if [[ ! -f /etc/tegu/allow_cq ]]				# unless this file is present, then we disable queue setting
then
	if [[ -n $queue ]]
	then
		echo "ignoring -q setting: htb queues not allowed   [OK]"
		queue=""
	fi
fi

# CAUTION: action options to send_ovs_fmods are probably order dependent, so be careful.
if (( ! one_switch ))
then
	# inbound -- only if both are not on the same switch
	send_ovs_fmod $forreal $host $timeout -p $(( 450 + pri_base )) --match $match_vlan $ip_type -m 0x0/0x7 $iexip -d $lmac -s $rmac $ib_lproto $ib_rproto --action $queue $idscp -M 0x01 -R ,0 -N $operation $cookie $bridge
	rc=$?
else
	if (( ! koe ))		# one switch and keep is off, no need to set dscp on the outbound fmod
	then
		odscp=""
	fi
fi

#outbound
if [[ -n $match_port ]]			# if we have an input port, we can drop the source mac match
then
	set -x
	send_ovs_fmod $forreal $host $timeout -p $(( 400 + vp_base + pri_base )) --match  $match_port $ip_type -m 0x0/0x7 $oexip -d $rmac $ob_lproto $ob_rproto --action $queue $odscp -M 0x01  -R ,0 -N $operation $cookie $bridge
	(( rc = rc + $? ))
	set +x
else
	set -x
	send_ovs_fmod $forreal $host $timeout -p $(( 400 + vp_base + pri_base )) --match  $match_port $ip_type -m 0x0/0x7 $oexip -s $lmac -d $rmac $ob_lproto $ob_rproto --action $queue $odscp -M 0x01  -R ,0 -N $operation $cookie $bridge
	(( rc = rc + $? ))
	set +x
fi

rm -f /tmp/PID$$.*
if (( rc ))
then
	exit 1
fi

exit 0
