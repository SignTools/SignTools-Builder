# iOS Signer Builder

This is a free and simple builder server for [ios-signer-service](https://github.com/SignTools/ios-signer-service). This project is the self-hosted alternative of [ios-signer-ci](https://github.com/SignTools/ios-signer-ci) - instead of using a Continuous Integration (CI) provider, this server turns one of your very own Macs into a builder used to pull, sign, and upload any iOS apps to your `ios-signer-service`.

You only need to configure one builder. If you already configured a CI provider as your builder, you don't need to do anything here. This project is aimed at people who want to have a self-hosted builder.

## Setup

All the steps should be performed on your builder Mac.

1. Install the following dependencies:
   - [Xcode](https://developer.apple.com/xcode/)
   - curl
   - p7zip (optional, but recommended)
2. Download the correct [binary release](https://github.com/SignTools/ios-signer-builder/releases)
3. Make the binary executable by running: `chmod +x ios-signer-builder`. Replace the name with the file that you just downloaded
4. Download the archive of `ios-signer-ci` and extract it in the same folder as the binary from the previous step. These will be your **signing files**. The whole step can be accomplished with the following commands:
   ```bash
   curl -sL https://github.com/SignTools/ios-signer-ci/archive/master.zip -o master.zip
   unzip master.zip
   rm master.zip
   ```

Important:

> :warning: **Remember to update the signing files from above every time that you update the signing service. Otherwise you may experience random issues.**

This server requires the use of an authentication key so that only the web service can control your builder. However, there is no built-in support for HTTPS or any other form of encryption. Therefore:

> :warning: **Anybody with access to the builder's network can potentially manipulate the builder to execute any code that they want on your machine.**

To prevent this, only deploy this server in a trusted environment, or even better, wrap the server in HTTPS yourself using a reverse proxy like nginx.

The authentication key has to be at least 8 characters long. Note it down - you will need to put it in your `ios-signer-service`'s configuration file later on.

## Running

When starting the server, you will want to use the same auth key and signing files from above and pass them as arguments:

```bash
./ios-signer-builder -key "SOME_SECRET_KEY" -files "ios-signer-ci-master"
```

The first time you run the server, you will have to [allow](https://www.macworld.co.uk/how-to/mac-app-unidentified-developer-3669596/) the unrecognized binary to run on your machine. After that it will run with no interruptions.

Additionally, the first time you attempt to sign an app using a developer account, macOS will ask you to grant permission for UI automation. You need to grant this permission or signing can't work. The prompt may break the current signing process, so just re-upload the app on the web service once more - it will work the next time.

For reference, these all of the arguments that will be used:

```bash
  -files string
    	Path to directory whose files will be included in each sign job. Should at least contain a signer script 'sign.sh'
  -host string
    	Listen host, empty for all
  -key string
    	Auth key the web service must use to talk to this server
  -port uint
    	Listen port (default 8090)
  -timeout uint
    	Job timeout in minutes (default 15)
```

You can always print them by running with `-help`.
