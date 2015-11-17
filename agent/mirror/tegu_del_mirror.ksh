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
#       Usage:     tegu_del_mirror [-v] <name>
#       Abstract:  This script deletes a mirror, named by <name>, from openvswitch.
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
#

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function findbridge
{
	ovs_sp2uuid -a | awk -v uuid=$1 '
		/^switch/ { br = $4 }
		/^port/ && $2 == uuid { print br }'
}

PATH=$PATH:/sbin:/usr/bin:/bin 		# must pick up agent augmented path
echo=:
if [ "$1" == "-v" ]
then
	shift
	echo=echo
fi

if [ $# != 1 ]
then
	echo "usage: tegu_del_mirror [-v] name" >&2
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

echo "tegu_del_mirror: mirror $mirrorname does not exist." >&2
rm -f /tmp/m$$
exit 3
