
import * as middleware from "./middleware.js";
import util from 'util';

const update = util.promisify(middleware.sendGameState);

middleware.start().then(
    () => {
        console.log("Starting Game!");
        startGame();
    },
    () => {
        console.error("ERROR STARTING GAME!");
    }
);

async function startGame(){
    var obj, res;
    while(true){
        obj = getRandomObject();
        
        res = await update(obj);
        if(res){
            console.log("Got a result!");
            console.log(res);
            console.log("\n\n");
        }
    }
}

function getRandomObject(){
    var res = {};

    for(var i = 0; i < 5; i ++){
        res[makeRandomString(Math.floor(Math.random() * 30) + 1)] = i;
    }

    return res;
}

function makeRandomString(length) {
    var res = [];
    var alphabet = "abcdefhikjlmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";
    for(var i = 0; i < length; i ++){
        res.push(alphabet[
            Math.floor(Math.random() * alphabet.length)
        ]);
    }

    return res.join('');
}

