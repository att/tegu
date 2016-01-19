#!/bin/ksh
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
#       Name:      tegu_del_mirror
#       Usage:     tegu_del_mirror [-o<options>] [-v] <name>
#       Abstract:  This script deletes a mirror, named by <name>, from openvswitch.
#
#                  The only currently valid option is -oflowmod, to delete a flowmod based mirror.
#
#       Author:    Robert Eby
#       Date:      04 February 2015
#
#       Mods:      04 Feb 2015 - created
#                  11 Feb 2015 - remove temp file
#                  25 Jun 2015 - Corrected PATH.
#                  15 Sep 2015 - Remove extra copyright
#                  19 Oct 2015 - Allow delete of mirrors from bridges other than br-int
#                  15 Nov 2015 - Fixed rather bad bug introduced w/last change
#                  23 Nov 2015 - Add -oflowmod option processing
#                  18 Jan 2016 - Hardened logic so that we don't inadvertently delete all flows
#                  19 Jan 2016 - Log if a null flow is found when deleting flows
#

function logit
{
	echo "$(date '+%s %Y/%m/%d %H:%M:%S') $argv0: $@" >&2
}

function findbridge
{
	ovs_sp2uuid -a | awk -v uuid=$1 '
		/^switch/ { br = $4 }
		/^port/ && $2 == uuid { print br }'
}

function option_set
{
	echo $options | tr ' ' '\012' | grep $1 > /dev/null
	return $?
}

function usage
{
	echo "usage: tegu_del_mirror [-o<options>] [-v] name" >&2
}

argv0=${0##*/}
PATH=$PATH:/sbin:/usr/bin:/bin 		# must pick up agent augmented path
echo=:
options=
while [[ "$1" == -* ]]
do
	if [[ "$1" == "-v" ]]
	then
		echo=echo
		shift
	elif [[ "$1" == -o* ]]
	then
		options=`echo $1 | sed -e 's/^-o//' -e 's/,/ /g'`
		shift
	else
		usage
		exit 1
	fi
done

if [ $# != 1 ]
then
	usage
	exit 1
fi
if [ ! -x /usr/bin/ovs-vsctl ]
then
	echo "tegu_del_mirror: ovs-vsctl is not installed or not executable." >&2
	exit 2
fi

sudo=sudo
[ "`id -u`" == 0 ] && sudo=

mirrorname=$1

# Special code to handle flowmod-ed mirror
if option_set flowmod
then
	# Find bridge with the GRE port
	$echo $sudo ovs-vsctl list port gre-$mirrorname
	$sudo ovs-vsctl list port gre-$mirrorname > /tmp/x$$ && {
		grep _uuid < /tmp/x$$ | sed 's/.*://' > /tmp/m$$
		rm /tmp/x$$
		bridgename=$(findbridge $(cat /tmp/m$$))

		# Find $GREPORT
		GREPORT=$(ovs_sp2uuid -a | grep gre-$mirrorname | cut -d' ' -f3)

		# Remove all flows with cookie=0xfaad from bridge that have actions=output:$GREPORT
		$sudo ovs-ofctl dump-flows $bridgename | grep "cookie=0xfaad.*output:$GREPORT," > /tmp/tdm.$$
		for flow in $(sed -e 's/.*priority=100,//' -e 's/ actions=.*//' </tmp/tdm.$$ | tr -d ' ')
		do
			if [ -n "$flow" ]
			then
				$echo $sudo ovs-ofctl del-flows $bridgename "$flow"
				$sudo ovs-ofctl del-flows $bridgename "$flow"
			else
				logit "Empty flow rule discovered"
				cat /tmp/tdm.$$ >&2
			fi
		done
		rm -f /tmp/tdm.$$ /tmp/m$$

		# Remove the GRE port
		$echo $sudo ovs-vsctl del-port $bridgename gre-$mirrorname
		$sudo ovs-vsctl del-port $bridgename gre-$mirrorname

		echo Mirror $mirrorname removed from bridge $bridgename.
		exit 0
	}
else
	$echo $sudo ovs-vsctl get mirror "$mirrorname" output_port _uuid
	$sudo ovs-vsctl get mirror "$mirrorname" output_port _uuid > /tmp/m$$ && {
		# get output_port UUID
		uuid=`sed -n 1p /tmp/m$$`
		bridgename=$(findbridge $uuid)

		# get name from uuid
		$echo $sudo ovs-vsctl list port $uuid
		pname=`$sudo ovs-vsctl list port $uuid | grep name | tr -d '" ' | cut -d: -f2`
		# if it is a GRE port, with the right name, remove port
		case "$pname" in
		gre-$mirrorname)
			$echo $sudo ovs-vsctl del-port $bridgename $pname
			$sudo ovs-vsctl del-port $bridgename $pname
			;;
		esac

		# get mirror UUID
		uuid=`sed -n 2p /tmp/m$$`
		$echo $sudo ovs-vsctl remove bridge $bridgename mirrors $uuid
		$sudo ovs-vsctl remove bridge $bridgename mirrors $uuid
		rm -f /tmp/m$$

		echo Mirror $mirrorname removed from bridge $bridgename.
		exit 0
	}
fi

echo "tegu_del_mirror: mirror $mirrorname does not exist." >&2
rm -f /tmp/m$$
exit 3
