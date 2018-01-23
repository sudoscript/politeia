# *Democratic Reddit*

Decreddit is a prototype of a social media platform with provable censorship and democratic checks and balances. It is built on top of [Politeia](https://github.com/decred/politeia), the Decred proposal system.

With Decreddit, users can:
* Prove that their social media posts have been censored by an admin or a moderator
* Call a referendum to overturn the censorship
* Prevent future censorship, if they achieve enough votes to overturn it

---

## How to Install and Run

Clone this repo. You'll need to save it to the regular Politeia directory to maintain package linkages, so be sure to push or save local changes before cloning.
To run the prototype, compile and launch the politeia daemon:
```
$ git clone git@github.com:sudoscript/politeia.git $GOPATH/src/github.com/decred/politeia
```

Then compile and launch the politeia daemon:
```
$ cd $GOPATH/src/github.com/decred/politeia
$ git checkout decreddit
$ dep ensure && go install -v ./... && LOGFLAGS=shortfile politeiad --testnet --rpcuser=user --rpcpass=pass
```

Download the server identity to client:
```
$ politeia -v -testnet -rpchost 127.0.0.1 identity
```
Accept default path by pressing `enter`.


### Publish a Post

Use the CLI to publish a post:
```
$ echo "Mods are NOT gods. We need new mods." > revolution.txt
$ politeia -v -testnet -rpchost 127.0.0.1 new revolution.txt
00: 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949 revolution.txt text/plain; charset=utf-8
Record submitted
  Censorship record:
    Merkle   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
    Token    : 1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d
    Signature: 13d5056515a02c0f85d2592978246d0f00570badcf53ac0f02bc5f7b0d27937174003503d6bf054400c7e65af2722d84b064f9335214d49d0a4546f79e813a0c
```

The post will be immediately public upon publishing. (The exact Token will be different.)
```
$ TOKEN=1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d

$ politeia -v -testnet -rpchost 127.0.0.1 getpublic $TOKEN
Public Post:
  Status     : public
  Timestamp  : 2018-01-23 04:26:09 +0000 UTC
  Censorship record:
    Merkle   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
    Token    : 1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d
    Signature: 13d5056515a02c0f85d2592978246d0f00570badcf53ac0f02bc5f7b0d27937174003503d6bf054400c7e65af2722d84b064f9335214d49d0a4546f79e813a0c
  Metadata   : []
  File (00)  :
    Name     : revolution.txt
    MIME     : text/plain; charset=utf-8
    Digest   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
```

Use admin priviledges to censor it (`rpcuser` and `rpcpass` required):
```
$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass censor $TOKEN
Set record status:
  Status   : censored
```

Any user can see that it's been censored:
```
$ politeia -v -testnet -rpchost 127.0.0.1 getcensored $TOKEN
Censored Post:
  Status     : censored
  Timestamp  : 2018-01-23 04:51:05 +0000 UTC
  Censorship record:
    Merkle   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
    Token    : 1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d
    Signature: 13d5056515a02c0f85d2592978246d0f00570badcf53ac0f02bc5f7b0d27937174003503d6bf054400c7e65af2722d84b064f9335214d49d0a4546f79e813a0c
  Metadata   : []
  File (00)  :
    Name     : revolution.txt
    MIME     : text/plain; charset=utf-8
    Digest   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
```

### Call a Referendum to Overturn Censorship

Any user can call a referendum on the act of censorship. Since referendum calls are signed, you need to provide a user identity file with the `-user` flag (if no file is provided, the default file user.json will be used).
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user user.json referendum $TOKEN
filename user1.json
Identity file found.
Referendum initiated on proposal: 1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d
```

While the referendum is open, any other user can vote either to `reverse` the censorship or to `uphold` it. Users provide their public key and a signed copy of the token, so they can only vote once. (The user who called the referendum cannot vote.)
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user user2.json vote reverse $TOKEN
filename user2.json
Identity file found.
Voted on referendum. Current status: referendum

$ politeia -v -testnet -rpchost 127.0.0.1 -user user3.json vote reverse $TOKEN
filename user3.json
Identity file found.
Voted on referendum. Current status: referendum
```

No one can vote after the referendum is closed. The default time is 20 seconds, for the purposes of demo.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -user user4.json vote uphold $TOKEN
filename user4.json
Identity file found.
400 Bad Request: map[errorcode:1 errorcontext:[Referendum is closed.]]
```

### See the Results and Take Action

Call for the votes to be tabulated and the appropriate action will automatically be taken.
```
$ politeia -v -testnet -rpchost 127.0.0.1 results $TOKEN
Votes for reversal: 2
Votes for upholding: 0

$ politeia -v -testnet -rpchost 127.0.0.1 getpublic $TOKEN
Public Post:
  Status     : public (final)
  Timestamp  : 2018-01-23 05:00:08 +0000 UTC
  Censorship record:
    Merkle   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
    Token    : 1cbc5d02fa26171b809d55174e99af3c55714e70bfdb769c3e68a78477a6f07d
    Signature: 13d5056515a02c0f85d2592978246d0f00570badcf53ac0f02bc5f7b0d27937174003503d6bf054400c7e65af2722d84b064f9335214d49d0a4546f79e813a0c
  Metadata   : []
  File (00)  :
    Name     : revolution.txt
    MIME     : text/plain; charset=utf-8
    Digest   : 40f711b9068bf1953a1b6351690d4e39420c32c49cd8bd35216c76712767a949
```

If the referendum passes, and censorship is overturned, it is impossible for the admins or moderators to censor again.
```
$ politeia -v -testnet -rpchost 127.0.0.1 -rpcuser user -rpcpass pass censor $TOKEN
400 Bad Request: invalid record status transition
```

