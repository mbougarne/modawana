const path = require('node:path');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');
const { readFileSync } = require('node:fs');

class AssetManifestPlugin {
  apply(compiler) {
    compiler.hooks.thisCompilation.tap('AssetManifestPlugin', (compilation) => {
      compilation.hooks.processAssets.tap(
        {
          name: 'AssetManifestPlugin',
          stage: compiler.webpack.Compilation.PROCESS_ASSETS_STAGE_REPORT,
        },
        () => {
          const files = compilation.entrypoints.get('app').getFiles();
          // Every entrypoint other than the SPA is a host-owned island bundle
          // for server-rendered pages (ADR 0002), listed so the Go server can
          // emit it only on pages that need it.
          const islands = {};
          for (const [name, entrypoint] of compilation.entrypoints) {
            if (name !== 'app') {
              islands[name] = entrypoint
                .getFiles()
                .filter((f) => !f.endsWith('.map'))
                .map((f) => '/' + f);
            }
          }
          const manifest = {
            version: 1,
            assets: Object.keys(compilation.assets)
              .filter(
                (f) =>
                  f.startsWith('assets/') && !f.endsWith('.LICENSE.txt') && !f.endsWith('.map'),
              )
              .map((f) => '/' + f),
            scripts: files.filter((f) => f.endsWith('.js')).map((f) => '/' + f),
            styles: files.filter((f) => f.endsWith('.css')).map((f) => '/' + f),
            ...(Object.keys(islands).length ? { islands } : {}),
          };
          compilation.emitAsset(
            'manifest.json',
            new compiler.webpack.sources.RawSource(JSON.stringify(manifest)),
          );
        },
      );
    });
  }
}
module.exports = (_, argv) => ({
  context: __dirname,
  entry: { app: './src/main.tsx', katex: './src/islands/katex.ts' },
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: 'assets/[name].[contenthash].js',
    assetModuleFilename: 'assets/[name].[contenthash][ext]',
    publicPath: '/',
    clean: true,
  },
  devtool: argv.mode === 'production' ? false : 'source-map',
  resolve: {
    alias: {
      '@ui': path.resolve(__dirname, '../ui/src'),
      react: path.resolve(__dirname, 'node_modules/react'),
      'react-dom': path.resolve(__dirname, 'node_modules/react-dom'),
    },
    extensions: ['.tsx', '.ts', '.js'],
    modules: [path.resolve(__dirname, 'node_modules'), 'node_modules'],
  },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        include: [path.resolve(__dirname, 'src'), path.resolve(__dirname, '../ui/src')],
        use: 'ts-loader',
      },
      {
        test: /\.css$/,
        use: [
          MiniCssExtractPlugin.loader,
          {
            loader: 'css-loader',
            options: { modules: { auto: /\.module\.css$/, namedExport: false } },
          },
        ],
      },
      { test: /\.(woff2|png|jpe?g|webp|svg)$/, type: 'asset/resource' },
    ],
  },
  plugins: [
    new MiniCssExtractPlugin({ filename: 'assets/[name].[contenthash].css' }),
    new HtmlWebpackPlugin({ template: './src/index.html', chunks: ['app'] }),
    new AssetManifestPlugin(),
  ],
  devServer: {
    server:
      process.env.BLOG_DEV_HTTPS === '1'
        ? {
            type: 'https',
            options: {
              cert: readFileSync(process.env.BLOG_DEV_TLS_CERT),
              key: readFileSync(process.env.BLOG_DEV_TLS_KEY),
            },
          }
        : 'http',
    host: '127.0.0.1',
    port: 3000,
    allowedHosts: ['localhost', '127.0.0.1'],
    historyApiFallback: true,
    proxy: [
      {
        context: ['/api', '/healthz', '/readyz', '/media'],
        target: 'http://' + (process.env.BLOG_LISTEN_ADDR || '127.0.0.1:8082'),
      },
    ],
  },
});
