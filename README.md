# Go Fast Copy - Fast file transfer over TCP using parallel LZ4 compression

## For when you gotta Go fast!
This repository provides simple client and server tools written in **Go** for the purpose of enabling fast file transfers over single TCP stream. File content is compressed by client using **LZ4** on the fly and decompressed by receiving end before persisting it on mass storage. The server application is simple single user tool with optional **AES-128 or 256** encryption for authentication and privacy when transferring files over untrusted networks.

## Performance

### Before you start
There is no simple universal answer to how much of a performance boost should you expect. Multiple factors such as storage speed, CPU speed, network speed and most importantly how compressible the data is, may affect the outcome. If the file is already compressed, you should not expect LZ4 to do much if anything at all to optimize its size further.

When using fast networks like 10GbE or faster, it's unlikely you'll see much benefit even if the data is highly compressible. With fast enough network the extra CPU time spent on compressing and decompressing data means it would be faster to just send it all uncompressed.

On the other hand, slower networks like Wifi, 1Gb or even 2.5Gb ethernet may see transfer time reduced quite significantly when sending highly compressible files. In my testing I've seen rates 2-3 times faster over 5GHz Wifi compared to sending same file uncompressed using NFS.

### Performance tuning
By default the client is set to use 4 workers for doing the compression (and encryption if enabled) whereas server is set to use 2. In both client and server you may specify number of workers using `-t #numworkers` command line argument. On modern CPUs you should not expect to see radical gains by throwing more cores at it. If you do enable encryption though, the added CPU overhead might have you opt for higher worker count. With older systems or low powered ones you will likely benefit more from increased parallelism.

Besides of workers, another consideration for performance is chunk size. Both client and server have the option `-c #size` to specify chunk size in **KB**. For both of them the default is **256KB**. On client side the parameter adjusts how big chunks are read from file at once and also how big chunks get compressed and ultimately sent to server. The parameter allows going up to 8MB chunks but what determines optimal size is storage and how compressible the data is. Bigger chunks may yield better performance but this is not universally true. On server side the parameter adjusts how big blocks get written to file at a time. The server does not need to commit full chunks at once so it's decoupled from size of chunk sent by client.

The client allows specifying DSCP/TOS using `-d #value` in case your network has QoS classification for traffic. **NOTE** that on _Windows_ operating systems by default the argument may not have any effect. In such case please refer to your OS documentation on how to enable overriding DSCP. On _Linux_ systems it should just work as most things usually do.

To enable Multipath TCP you can set the `-m` flag on both client and server. Make sure your OS supports MPTCP 
and your network settings are configured in such manner that you can make use of it. On most modern _Linux_ 
distros it is most likely enabled by default. Please refer to your OS documentation for more. Actual performance 
implications of using MPTCP may greatly vary and it's not a given that you will see much in terms of scaling.
Based on testing you could potentially see better results by doing some tuning such as increasing TCP window.
Please refer to your OS documentation for how to configure MPTCP for throughput.

## Usage
Minimal usage for server requires specifying root folder for storing received files to. This is done with the `-r #path` command line argument.

To store all received files in _/home/user/backups_ you would pass the following parameter:
```
server -r /home/user/backups
```

Minimal usage for client requires specifying target host and file path of source file. These are done using the `-a #address` and `-f #path` command line arguments.

To send file located in _C:\Generated\statistics.json_ to host at _192.168.1.1_ you would do the following:
```
client -a 192.168.1.1 -f C:\Generated\statistics.json
```

To enable AES 128 or 256 encryption you have to use the `-k #key` argument on both client and server to specify pre-shared key used in encryption. They key must be either 16 characters long for AES128 or 32 characters long for AES256.

To enable _AES128_ you would enter matching key which is 16 characters in length:
```
server -k xs6ow78RPlHZ2ffC
client -k xs6ow78RPlHZ2ffC
```

To enable _AES256_ you would enter matching key which is 32 characters in length:
```
server -k RikSNWp98uiHRYBlJcEzqaL0ucxj6F07
client -k RikSNWp98uiHRYBlJcEzqaL0ucxj6F07
```

## 3rd party libraries
Go Fast Copy is using following 3rd party libraries:

[_Golang argparse_ by Alexey Kamenskiy (MIT license)](https://github.com/akamensky/argparse)

[_lz4 compression in pure Go_ by Pierre Curto (BSD-3-Clause license)](https://github.com/pierrec/lz4)