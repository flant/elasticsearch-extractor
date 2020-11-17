# extractor
Extractor is a web tool for an extracting specific indices from the indicated repository, from the snapshot identified by users choice.

Requires Elasticsearch v7.0 or greater.


## INSTALL ##

To install *extractor* on Linux use the following commands:

    $ git clone https://github.com/uzhinskiy/extractor.git
    $ cd extractor
    $ make

## USAGE ##

    $ sudo cp ./build/extractor /usr/local/sbin/
    $ sudo cp main.yml /usr/local/etc/extractor.yml
    $ sudo cp ./scripts/extractor.service /etc/systemd/system/
    $ edit /usr/local/etc/extractor.yml
    $ sudo systemctl daemon-reload && systemctl start extractor
    $ sudo systemctl enable extractor
