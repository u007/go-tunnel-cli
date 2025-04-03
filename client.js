const net = require('net');

const checkConnection = () => {
  return new Promise((resolve, reject) => {
    const socket = new net.Socket();
    
    socket.setTimeout(5000);  // Set a 5-second timeout

    socket.connect(process.env.SSH_LOCALPORT, 'localhost', () => {
      socket.destroy();
      resolve(true);
    });

    socket.on('error', (error) => {
      socket.destroy();
      resolve(false);
    });

    socket.on('timeout', () => {
      socket.destroy();
      resolve(false);
    });
  });
};

const portStr = process.env.SSH_LOCALPORT || ''
checkConnection(portStr)
  .then((canConnect) => {
    if (canConnect) {
      console.log(`Successfully connected to port ${portStr}`);
    } else {
      console.error(`Failed to connect to port ${portStr}`);
    }
  })
  .catch((error) => {
    console.error('An error occurred:', error);
  });
