import { Sequelize } from 'sequelize';
import { describe, it, expect, beforeAll, afterAll } from 'bun:test';
import dotenv from 'dotenv';
import path from 'path';

// Load environment variables from .env file located in the parent directory
dotenv.config({ path: path.resolve(__dirname, 'test.env') });

describe('Sequelize PostgreSQL Connection', () => {
  let sequelize: Sequelize;

  beforeAll(() => {
    console.log('env', process.env)
    const dbName = process.env.DB_NAME;
    const dbUser = process.env.DB_USER;
    const dbPassword = process.env.DB_PASSWORD;
    const dbHost = process.env.DB_HOST;
    const dbPort = Number.parseInt(process.env.SSH_LOCALPORT || '5432', 10)

    if (!dbName || !dbUser || !dbPassword || !dbHost) {
        console.warn('One or more required database environment variables (DB_NAME, DB_USER, DB_PASSWORD, DB_HOST) are missing. Skipping connection test.');
        // Initialize sequelize with dummy values or handle as needed if you want tests to fail explicitly
        sequelize = new Sequelize('dummy', 'dummy', 'dummy', { dialect: 'postgres', host: 'localhost', port: 5432 });
        throw new Error('One or more required database environment variables (DB_NAME, DB_USER, DB_PASSWORD, DB_HOST) are missing.');
    }
    console.log('DB config:', { dbName, dbUser, dbPassword, dbHost, dbPort })
    sequelize = new Sequelize(dbName, dbUser, dbPassword, {
      host: dbHost,
      port: dbPort,
      dialect: 'postgres',
      logging: false, // Disable logging for tests unless needed
      dialectOptions: {
        // Add any specific dialect options if necessary
        // e.g., ssl: { require: true, rejectUnauthorized: false } for SSL
      },
    });
  });

  afterAll(async () => {
    // Ensure the connection is closed after tests run
    if (sequelize && sequelize.close) {
        try {
            await sequelize.close();
            console.log('Sequelize connection closed.');
        } catch (error) {
            console.error('Error closing Sequelize connection:', error);
        }
    }
  });

  it('should connect to the PostgreSQL database successfully', async () => {
    // Skip test if sequelize wasn't properly initialized due to missing env vars
     if (!process.env.DB_NAME || !process.env.DB_USER || !process.env.DB_PASSWORD || !process.env.DB_HOST) {
        console.warn('Skipping connection test due to missing environment variables.');
        expect(true).toBe(true); // Pass the test trivially
        return;
    }

    try {
      await sequelize.authenticate();
      console.log('Connection has been established successfully.');
      // Expect no error to be thrown for a successful connection
      expect(true).toBe(true);


      // list tables
      const tables = await sequelize.query("SELECT * FROM pg_catalog.pg_tables WHERE schemaname != '' AND schemaname != ''", { type: Sequelize.QueryTypes.SELECT });
      console.log('Tables:', tables.map(table => table.tablename));
      expect(tables.length).toBeGreaterThan(0);

    } catch (error) {
      console.error('Unable to connect to the database:', error);
      // Force the test to fail if authentication fails
      expect(error).toBeUndefined();
    }
  });
});

