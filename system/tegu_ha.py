#!/usr/bin/env python
# vi: ts=4 sw=4:

#	Mnemonic:	tegu_ha.py
#	Abstract:	High availability script for tegu. Pings other tegu's in the standby_list. If no other tegu is running,
#               then makes the current tegu active. If it finds multiple tegu running, then determines whether current
#               tegu should be shut down or not based on db timestamps. Assumes that db checkpoints from other tegus do
#               not override our checkpoints. 
#
# 	Date:		15 December 2015
#	Author:		Kaustubh Joshi
#	Mod:		2014 15 Dec - Created script
# -----------------------------------------------------------------------------------------------------------------------------

#  Algorithm
# -----------
# The tegu_ha script runs on each tegu node in a continuous loop checking for hearbeats
# from all the tegu nodes found in the /etc/tegu/standby_list once every 5 secs (default).
# A heartbeat is obtained by invoking the "tegu ping" command via the HTTP API. If exactly
# one tegu is running, the script does nothing. If no tegu is running, then the local tegu
# is activated after waiting for 5*priority seconds to let a higher priority tegu take over
# first. A tegu node's priority is determined by its order in the standby_list file.
# If the current node's tegu is running and another tegu is found, then a conflict resolution
# process is invoked whereby the last modified timestamps from checkpoint files from both tegu's
# are compared, and the tegu with the older checkpoint is deactivated. The rationale is that
# the F5 only keeps a single tegu active at a time, so the tegu with the most recent checkpoint
# ought to be the active one.

import time
import os
import socket
import subprocess

# Directory locations
TEGU_ROOT=os.getenv('TEGU_ROOT', '/var')                 # Tegu root dir
LIBDIR=os.getenv('TEGU_LIBD', TEGU_ROOT+'/lib/tegu')
LOGDIR=os.getenv('TEGU_LOGD', TEGU_ROOT+'/log/tegu')
ETCDIR=os.getenv('TEGU_ETCD', TEGU_ROOT+'/etc/tegu')
CKPTDIR=os.getenv('TEGU_CKPTD', LIBDIR+'/chkpt')         # Checkpoints directory
LOGFILE=LOGDIR+'/tegu.std'				                 # log file for this script

# Tegu config parameters
TEGU_PORT=os.getenv('TEGU_PORT', 29444)		             # tegu's api listen port
TEGU_USER=os.getenv('TEGU_USER', 'tegu')                 # by default we run only under tegu user
TEGU_PROTO='http'

SSH_CMD='ssh -o StrictHostKeyChecking=no %s@%s '
DEACTIVATE_CMD='/usr/bin/tegu_standby on;' + \           # Command to kill tegu
    'killall tegu; killall tegu_agent'
ACTIVATE_CMD='/usr/bin/tegu_standby off;' + \
    '/usr/bin/start_tegu; /usr/bin/start_tegu_agent'     # Command to start tegu
SYNC_CMD='/usr/bin/tegu_synch'                           # Command to force checkpoint synchronization

# HA Configuration
HEARTBEAT_SEC=5                                          # Heartbeat interval in seconds
PRI_WAIT_SEC=5                                           # Backoff to let higher priority tegu take over
STDBY_LIST=ETCDIR+'/standby_list'                        # list of other hosts that might run tegu
STDBY_FILE=ETCDIR+'standby'			                     # if present then this is a standby machine and we don't start
VERBOSE=0

ignore_cert=""						# -i will set this to -k for curl

def warn(msg):
    sys.stderr.write(msg)

def ssh_cmd(host):
    if host != '':
        return SSH_CMD % (TEGU_USER, host)
    return ''
        
def get_checkpoint(host=''):
    ssh_prefix = ssh_cmd(host)

    try:
        subprocess.check_call(ssh_prefix + SYNC_CMD, shell="True")
        return True
    except subprocess.CalledProcessError:
        warn("Could not sync chkpts from %s" % host)
    return False

# Returns True if host should be active as
# opposed to current node
def should_be_active(host):
    # Pull latest checkpoint from remote node
    # If there's an error, the other guy shouldn't be primary
    if not get_checkpoint(host):
        return False
    if not get_checkpoint():
        return True


    # Check if we or they have latest checkpoint
    try:
        # Get clock skew
        time_r = subprocess.read_call(ssh_cmd(host) + 'date +%s')
        time_l = subprocess.read_call(ssh_cmd('') + 'date +%s')
        skew = time_l-time_r

        # Get ours and their checkpoint timestamps
        ts_r = subprocess.read_call('ls -t ' + LIBDIR + '/chkpt_synch.' + host + '.*.tgz' +
                                    '| head -1 | read synch_file;' +
                                    'tar -t -v --full-time -f $synch_file')
        ts_l = subprocess.read_call('ls -tF ' + CKPTDIR + '/* | grep -v \/')

        return (ts_r+skew > ts_l or (ts_r+skew == ts_l and host < socket.getfqdn())) 

    except subprocess.CalledProcessError:
        warn("Could not get chkpt timestamps")

    return False


    # Now check who has the latest chkpoint time
    
#     chkpt_x = get_snapshot(x)
#     chkpt_i = get_my_snapshot()
#     skew = get_skew(x)
#     return chkpt_x.timestamp + skew > chkpt_i.timestamp or
#         chkpt_x.timestamp + skew == chkpt_i.timestamp and x < i  
	return False

# Return True if tegu is running on host
# If host is None, check if tegu is running on current host
# Use ping API check, standby file may be inconsistent
def is_active(host='localhost'):
    curl_str = 'curl --connect-timeout 3 -s -d "ping" %s://%s:%d/tegu/api ' + \
        '| grep -q -i pong' % (TEGU_PROTO, host, TEGU_PROTO)    
    try:
        subprocess.check_call(curl_str, shell=True)
        return True
    except subprocess.CalledProcessError:
        return False

# Deactivate tegu on a given host. If host is omitted, local tegu is stopped.
# Returns True if successful, False on error.
def deactivate_tegu(host=''):
    ssh_prefix = ssh_cmd(host)
    try:
        subprocess.check_call(ssh_prefix + DEACTIVATE_CMD, shell=True)
        return True
    except subprocess.CalledProcessError:
        return False

# Deactivate tegu on a given host. If host is omitted, local tegu is stopped.
# Returns True if successful, False on error.
def activate_tegu(host):
    if host != '':
        host =  SSH_CMD % (TEGU_USER, host)
    try:
        subprocess.check_call(host + ACTIVATE_CMD, shell=True)
        return True
    except subprocess.CalledProcessError:
        return False

def main_loop(standby_list, me, priority):
    priority_wait = False
	while True:
        if not priority_wait:
            # Normal heartbeat
            time.sleep(HEARTBEAT_SEC)
        else:
            # No tegu running. Wait for higher priority tegu to activate.
            time.sleep(PRI_WAIT_SEC*priority)            

        i_am_active = is_active()
        any_active = i_am_active

        # Check for active tegus
        for host in standby_list:
            if host == me:
                continue

            host_active = is_active(host)

            # Check for split brain: 2 tegus active
            if i_am_active and host_active:
                host_active = should_be_active(host)
                if host_active:
                    deactivate_tegu()      # Deactivate myself
                    i_am_active = False
                else:
                    deactivate_tegu(host)  # Deactivate other tegu

            # Track that at-least one tegu is active
            any_active = any_active or host_active

        # If no active tegu, then we must try
        if not any_active:
            if priority_wait or priority == 0:
                priority_wait=False
                activate_tegu()            # Start local tegu
            else:
                priority_wait=True
    # end loop

        
if __name__ == 'main':

    # Ready list of standby tegu nodes and find us
	standby_list = [l.strip() for l in open(STDBY_FILE, 'r')]
    me = socket.getfqdn()
    
    try:
        priority = standby_list.index(me)
        standby_list.remove(me)
    except ValueError:
        sys.stderr.write("Could not find host "+me+" in standby list")
        sys.exit(1)

    # Loop forever listening to heartbeats
    main_loop(standby_list, me, priority)


